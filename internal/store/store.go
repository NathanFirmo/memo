package store

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/NathanFirmo/memo/internal/config"
	"github.com/NathanFirmo/memo/internal/memory"
	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	sqlitevec.Auto()
}

//go:embed schema.sql
var schemaSQL string

type OpenOptions struct {
	Home string
	DB   string
}

type Store struct {
	db *sql.DB
}

type MemoryInput struct {
	Title string
	Body  string
}

type SearchOptions struct {
	Query          string
	Limit          int
	Vector         []float32
	MinVectorScore float64
}

type SearchResult struct {
	Memory      memory.Memory `json:"memory"`
	Score       float64       `json:"score"`
	FTSScore    float64       `json:"fts_score"`
	VectorScore float64       `json:"vector_score"`
}

type Stats struct {
	Memories int64 `json:"memories"`
}

const DefaultMinVectorScore = 0.76

func Open(opts OpenOptions) (*Store, error) {
	if opts.Home == "" || opts.DB == "" {
		return nil, fmt.Errorf("home and db path are required")
	}
	if err := os.MkdirAll(opts.Home, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(opts.DB), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", opts.DB+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return err
	}
	if err := s.SetSetting("embedding_model", config.DefaultEmbeddingModel); err != nil {
		return err
	}
	_, _ = s.db.Exec(`INSERT OR IGNORE INTO settings(key, value) VALUES ('schema_version', '1')`)
	return nil
}

func (s *Store) SQLiteVersions() (sqliteVersion, vecVersion string, err error) {
	err = s.db.QueryRow(`SELECT sqlite_version(), vec_version()`).Scan(&sqliteVersion, &vecVersion)
	return sqliteVersion, vecVersion, err
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO settings(key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at
	`, key, value, now())
	return err
}

func (s *Store) Setting(key string) (string, bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (s *Store) EmbeddingDimensions() (int, bool, error) {
	value, ok, err := s.Setting("embedding_dimensions")
	if err != nil || !ok {
		return 0, false, err
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, false, err
	}
	return n, true, nil
}

func (s *Store) EnsureVectorTable(dim int) error {
	if dim <= 0 {
		return fmt.Errorf("embedding dimension must be positive")
	}
	if _, err := s.db.Exec(fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS memory_vec USING vec0(embedding float[%d] distance_metric=cosine)`, dim)); err != nil {
		return err
	}
	return s.SetSetting("embedding_dimensions", strconv.Itoa(dim))
}

func (s *Store) AddMemory(ctx context.Context, input MemoryInput, vector []float32) (int64, error) {
	input = normalizeInput(input)
	if input.Title == "" && input.Body == "" {
		return 0, fmt.Errorf("title or body is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
		INSERT INTO memories(title, body)
		VALUES (?, ?)
	`, input.Title, input.Body)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if len(vector) > 0 {
		if err := insertVector(ctx, tx, id, vector); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func (s *Store) MemoriesMissingEmbeddings(ctx context.Context, limit int) ([]memory.Memory, error) {
	if limit <= 0 {
		limit = -1
	}
	hasVectorTable, err := s.tableExists("memory_vec")
	if err != nil {
		return nil, err
	}
	query := `
		SELECT id, title, body, created_at
		FROM memories
		ORDER BY id
		LIMIT ?
	`
	if hasVectorTable {
		query = `
			SELECT m.id, m.title, m.body, m.created_at
			FROM memories m
			WHERE NOT EXISTS (
				SELECT 1 FROM memory_vec v WHERE v.rowid = m.id
			)
			ORDER BY m.id
			LIMIT ?
		`
	}
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []memory.Memory
	for rows.Next() {
		var item memory.Memory
		var createdAt string
		if err := rows.Scan(&item.ID, &item.Title, &item.Body, &createdAt); err != nil {
			return nil, err
		}
		if err := parseTimes(&item, createdAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertMemoryVector(ctx context.Context, id int64, vector []float32) error {
	if len(vector) == 0 {
		return fmt.Errorf("embedding vector is required")
	}
	if err := s.EnsureVectorTable(len(vector)); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_vec WHERE rowid = ?`, id); err != nil {
		return err
	}
	if err := insertVector(ctx, tx, id, vector); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Search(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	candidates := map[int64]*SearchResult{}
	if strings.TrimSpace(opts.Query) != "" {
		fts, err := s.searchFTS(ctx, opts)
		if err != nil {
			return nil, err
		}
		merge(candidates, fts)
	}
	if len(opts.Vector) > 0 {
		vec, err := s.searchVector(ctx, opts)
		if err == nil {
			merge(candidates, vec)
		}
	}
	results := make([]SearchResult, 0, len(candidates))
	minVectorScore := opts.MinVectorScore
	if minVectorScore == 0 && len(opts.Vector) > 0 {
		minVectorScore = DefaultMinVectorScore
	}
	for _, item := range candidates {
		if minVectorScore > 0 && item.VectorScore > 0 && item.VectorScore < minVectorScore && item.FTSScore == 0 {
			continue
		}
		item.Score = item.FTSScore + item.VectorScore
		results = append(results, *item)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return results, nil
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM memories`).Scan(&stats.Memories); err != nil {
		return stats, err
	}
	return stats, nil
}

func (s *Store) searchFTS(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.title, m.body, m.created_at,
		       1.0 / (1.0 + bm25(memory_fts)) AS fts_score
		FROM memory_fts
		JOIN memories m ON m.id = memory_fts.rowid
		WHERE memory_fts MATCH ?
		ORDER BY bm25(memory_fts)
		LIMIT ?
	`, quoteFTS(opts.Query), opts.Limit*3)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var item SearchResult
		if err := scanMemoryWithExtra(rows, &item.Memory, &item.FTSScore); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) searchVector(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	blob, err := sqlitevec.SerializeFloat32(opts.Vector)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.title, m.body, m.created_at,
		       1.0 / (1.0 + v.distance) AS vector_score
		FROM memory_vec v
		JOIN memories m ON m.id = v.rowid
		WHERE v.embedding MATCH ? AND k = ?
		ORDER BY v.distance
	`, blob, opts.Limit*3)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SearchResult
	for rows.Next() {
		var item SearchResult
		if err := scanMemoryWithExtra(rows, &item.Memory, &item.VectorScore); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) tableExists(name string) (bool, error) {
	var exists int
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`, name).Scan(&exists)
	return exists == 1, err
}

func insertVector(ctx context.Context, tx *sql.Tx, id int64, vector []float32) error {
	blob, err := sqlitevec.SerializeFloat32(vector)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memory_vec(rowid, embedding) VALUES (?, ?)`, id, blob)
	return err
}

func normalizeInput(input MemoryInput) MemoryInput {
	input.Title = strings.TrimSpace(input.Title)
	input.Body = strings.TrimSpace(input.Body)
	return input
}

func merge(dst map[int64]*SearchResult, src []SearchResult) {
	for _, item := range src {
		existing := dst[item.Memory.ID]
		if existing == nil {
			copy := item
			dst[item.Memory.ID] = &copy
			continue
		}
		if item.FTSScore > existing.FTSScore {
			existing.FTSScore = item.FTSScore
		}
		if item.VectorScore > existing.VectorScore {
			existing.VectorScore = item.VectorScore
		}
	}
}

func quoteFTS(query string) string {
	tokens := ftsQueryTokens(query)
	if len(tokens) == 0 {
		return `""`
	}
	terms := make([]string, 0, len(tokens))
	for _, token := range tokens {
		terms = append(terms, token+"*")
	}
	return strings.Join(terms, " AND ")
}

func ftsQueryTokens(query string) []string {
	words := strings.FieldsFunc(normalizeFTSQuery(query), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	seen := map[string]bool{}
	var tokens []string
	for _, word := range words {
		if len(word) < 3 || seen[word] {
			continue
		}
		seen[word] = true
		tokens = append(tokens, word)
	}
	return tokens
}

func normalizeFTSQuery(query string) string {
	query = strings.ToLower(query)
	replacements := map[string]string{
		"á": "a", "à": "a", "â": "a", "ã": "a",
		"é": "e", "ê": "e",
		"í": "i",
		"ó": "o", "ô": "o", "õ": "o",
		"ú": "u",
		"ç": "c",
	}
	for from, to := range replacements {
		query = strings.ReplaceAll(query, from, to)
	}
	return query
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMemoryWithExtra(row rowScanner, m *memory.Memory, extra *float64) error {
	var createdAt string
	if err := row.Scan(&m.ID, &m.Title, &m.Body, &createdAt, extra); err != nil {
		return err
	}
	return parseTimes(m, createdAt)
}

func parseTimes(m *memory.Memory, createdAt string) error {
	var err error
	m.CreatedAt, err = parseTime(createdAt)
	return err
}

func parseTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000Z", "2006-01-02T15:04:05Z"} {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid timestamp %q", value)
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func MigrationNames() ([]string, error) {
	return []string{"0001_initial.sql"}, nil
}
