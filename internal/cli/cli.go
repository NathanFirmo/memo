package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/NathanFirmo/memo/internal/agent"
	"github.com/NathanFirmo/memo/internal/config"
	"github.com/NathanFirmo/memo/internal/embed"
	"github.com/NathanFirmo/memo/internal/mcp"
	"github.com/NathanFirmo/memo/internal/store"
)

func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "add":
		return runAdd(args[1:], stdout)
	case "search":
		return runSearch(args[1:], stdout)
	case "mcp":
		return runMCP(args[1:], stdout)
	case "doctor":
		return runDoctor(args[1:], stdout)
	case "stats":
		return runStats(args[1:], stdout)
	case "agent":
		return runAgent(args[1:], stdout)
	case "help", "-h", "--help":
		printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func commonFlags(name string) (*flag.FlagSet, *string, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	home := fs.String("home", "", "override Memo home directory")
	db := fs.String("db", "", "override Memo SQLite database path")
	return fs, home, db
}

func openStore(home, db string) (*store.Store, config.Paths, error) {
	paths, err := config.Resolve(home, db)
	if err != nil {
		return nil, config.Paths{}, err
	}
	s, err := store.Open(store.OpenOptions{Home: paths.Home, DB: paths.DB})
	return s, paths, err
}

func runAdd(args []string, stdout io.Writer) error {
	fs, home, db := commonFlags("add")
	title := fs.String("title", "", "memory title")
	body := fs.String("body", "", "memory body")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *body == "" && fs.NArg() > 0 {
		*body = strings.Join(fs.Args(), " ")
	}

	s, _, err := openStore(*home, *db)
	if err != nil {
		return err
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	vector := embedIfPossible(ctx, s, *title+"\n"+*body)
	id, err := s.AddMemory(ctx, store.MemoryInput{
		Title: *title, Body: *body,
	}, vector)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "memory added\nid: %d\n", id)
	return nil
}

func runSearch(args []string, stdout io.Writer) error {
	fs, home, db := commonFlags("search")
	limit := fs.Int("limit", 10, "maximum result count")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.Join(fs.Args(), " ")
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("search query is required")
	}

	s, _, err := openStore(*home, *db)
	if err != nil {
		return err
	}
	defer s.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	vector := embedIfPossible(ctx, s, query)
	results, err := s.Search(ctx, store.SearchOptions{
		Query: query, Limit: *limit, Vector: vector,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(stdout).Encode(results)
	}
	for _, result := range results {
		fmt.Fprintf(stdout, "[%d] %.3f %s\n%s\n\n", result.Memory.ID, result.Score, result.Memory.Title, result.Memory.Body)
	}
	return nil
}

func runMCP(args []string, stdout io.Writer) error {
	fs, home, db := commonFlags("mcp")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, _, err := openStore(*home, *db)
	if err != nil {
		return err
	}
	defer s.Close()
	return mcp.Serve(context.Background(), s, stdout, nil)
}

func runDoctor(args []string, stdout io.Writer) error {
	fs, home, db := commonFlags("doctor")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, paths, err := openStore(*home, *db)
	if err != nil {
		return err
	}
	defer s.Close()

	fmt.Fprintf(stdout, "home: %s\n", paths.Home)
	fmt.Fprintf(stdout, "db: %s\n", paths.DB)
	sqliteVersion, vecVersion, err := s.SQLiteVersions()
	if err != nil {
		fmt.Fprintf(stdout, "sqlite: unavailable (%v)\n", err)
	} else {
		fmt.Fprintf(stdout, "sqlite_version: %s\nvec_version: %s\n", sqliteVersion, vecVersion)
	}
	if dim, ok, err := s.EmbeddingDimensions(); err == nil && ok {
		fmt.Fprintf(stdout, "embedding_model: %s\nembedding_dimensions: %d\n", config.DefaultEmbeddingModel, dim)
	} else {
		fmt.Fprintf(stdout, "embedding_model: %s\nembedding_dimensions: unset\n", config.DefaultEmbeddingModel)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	vec, err := embed.NewClient("", "").Embed(ctx, "memo doctor")
	if err != nil {
		fmt.Fprintf(stdout, "ollama: unavailable (%v)\n", err)
		return nil
	}
	fmt.Fprintf(stdout, "ollama: ok\nollama_dimensions: %d\n", len(vec))
	return nil
}

func runStats(args []string, stdout io.Writer) error {
	fs, home, db := commonFlags("stats")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, _, err := openStore(*home, *db)
	if err != nil {
		return err
	}
	defer s.Close()
	stats, err := s.Stats(context.Background())
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(stats)
}

func runAgent(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: memo agent install|uninstall|instructions")
	}
	switch args[0] {
	case "install":
		return runAgentInstall(args[1:], stdout)
	case "uninstall":
		return runAgentUninstall(args[1:], stdout)
	case "instructions":
		fmt.Fprintln(stdout, agent.Instructions())
		return nil
	default:
		return fmt.Errorf("unknown agent command %q", args[0])
	}
}

func runAgentInstall(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("agent install", flag.ContinueOnError)
	agentName := fs.String("agent", "all", "agent to configure: codex, claude or all")
	home := fs.String("home", "", "override user home directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return agent.Installer{Stdout: stdout}.Install(context.Background(), agent.InstallOptions{Agent: *agentName, Home: *home})
}

func runAgentUninstall(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("agent uninstall", flag.ContinueOnError)
	agentName := fs.String("agent", "all", "agent to unconfigure: codex, claude or all")
	home := fs.String("home", "", "override user home directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return agent.Installer{Stdout: stdout}.Uninstall(context.Background(), agent.InstallOptions{Agent: *agentName, Home: *home})
}

func embedIfPossible(ctx context.Context, s *store.Store, text string) []float32 {
	vector, err := embed.NewClient("", "").Embed(ctx, text)
	if err != nil || len(vector) == 0 {
		return nil
	}
	_ = s.EnsureVectorTable(len(vector))
	return vector
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Memo is a tiny local memory tool for agents.

Usage:
  memo add --title "..." --body "..."
  memo search "query"
  memo stats
  memo doctor
  memo mcp
  memo agent install --agent codex

Options:
  --home PATH   override Memo home directory
  --db PATH     override Memo SQLite database path`)
}
