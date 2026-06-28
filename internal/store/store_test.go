//go:build sqlite_fts5

package store

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/NathanFirmo/memo/internal/embed"
)

func TestAddAndSearchMemory(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(OpenOptions{
		Home: filepath.Join(dir, "home"),
		DB:   filepath.Join(dir, "memo.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	_, err = s.AddMemory(context.Background(), MemoryInput{
		Title: "Concise answers",
		Body:  "Use direct answers without extra ceremony.",
	}, nil)
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}

	results, err := s.Search(context.Background(), SearchOptions{
		Query: "concise answers",
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Memory.Title != "Concise answers" {
		t.Fatalf("unexpected result: %q", results[0].Memory.Title)
	}
}

func TestStats(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(OpenOptions{Home: dir, DB: filepath.Join(dir, "memo.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	if _, err := s.AddMemory(context.Background(), MemoryInput{Body: "one"}, nil); err != nil {
		t.Fatalf("add memory: %v", err)
	}
	stats, err := s.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Memories != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestOllamaSemanticSearchQuality(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	embedClient := embed.NewClient("", "")
	sampleVec, err := embedClient.Embed(ctx, "memo semantic search integration test")
	if err != nil {
		t.Skipf("Ollama embedding unavailable: %v", err)
	}

	dir := t.TempDir()
	s, err := Open(OpenOptions{Home: dir, DB: filepath.Join(dir, "memo.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	if err := s.EnsureVectorTable(len(sampleVec)); err != nil {
		t.Fatalf("ensure vector table: %v", err)
	}

	for _, item := range semanticCorpus {
		vector, err := embedClient.Embed(ctx, item.title+"\n"+item.body)
		if err != nil {
			t.Fatalf("embed %q: %v", item.title, err)
		}
		if _, err := s.AddMemory(ctx, MemoryInput{
			Title: item.title,
			Body:  item.body,
		}, vector); err != nil {
			t.Fatalf("add %q: %v", item.title, err)
		}
	}

	for _, tc := range semanticCases {
		t.Run(tc.name, func(t *testing.T) {
			queryVec, err := embedClient.Embed(ctx, tc.query)
			if err != nil {
				t.Fatalf("embed query: %v", err)
			}
			results, err := s.Search(ctx, SearchOptions{
				Query:          tc.query,
				Limit:          8,
				Vector:         queryVec,
				MinVectorScore: 0.68,
			})
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if !hasCategory(results, tc.wantCategory, 3) {
				t.Fatalf("missing expected category %q in top 3:\n%s", tc.wantCategory, formatSearchResults(results))
			}
			for _, banned := range tc.bannedCategories {
				if hasCategory(results, banned, 5) {
					t.Fatalf("banned category %q appeared in top 5:\n%s", banned, formatSearchResults(results))
				}
			}
		})
	}

	t.Run("unrelated query returns nothing above threshold", func(t *testing.T) {
		query := "quantum orchid submarine lullaby with marble clouds"
		queryVec, err := embedClient.Embed(ctx, query)
		if err != nil {
			t.Fatalf("embed unrelated query: %v", err)
		}
		results, err := s.Search(ctx, SearchOptions{
			Query:          query,
			Limit:          5,
			Vector:         queryVec,
			MinVectorScore: 0.78,
		})
		if err != nil {
			t.Fatalf("search unrelated: %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("unexpected false positives:\n%s", formatSearchResults(results))
		}
	})
}

type semanticMemory struct {
	category string
	title    string
	body     string
}

var semanticCorpus = []semanticMemory{
	{"auth", "SSO migration 2FA delivery failure", "After moving corporate users to SSO, several people cannot receive two factor login codes. Check login mode, identity provider routing, email aliases and fallback authentication before changing account records."},
	{"auth", "Keycloak duplicate email remediation", "When identity import reports duplicate email conflicts, compare canonical email, legacy email and migration email fields before updating the identity provider account."},
	{"auth", "Passwordless login troubleshooting", "For users stuck in passwordless login, inspect token expiry, one time code delivery, account lock status and recent authentication provider changes."},
	{"finance", "Payout batch reconciliation mismatch", "Ledger totals can diverge when payment batch settlement uses net amount in one system and gross amount in another. Compare batch reference, settlement status and movement rows."},
	{"finance", "Wallet transfer balance repair", "Internal wallet transfers require paired debit and credit movements. Recompute account balance from source and destination movements when historical transfers are missing."},
	{"finance", "Invoice capture retry procedure", "If card invoice capture fails, retry only idempotent payment attempts and verify processor reference, authorization code and settlement timestamp."},
	{"testing", "React component flaky test", "A flaky React test around loading state should wait for accessible UI changes instead of fixed timeouts, and isolate network mocks per test case."},
	{"testing", "Go integration test database setup", "Use a temporary SQLite database per test, seed deterministic records and avoid depending on shared local state."},
	{"testing", "CI cache invalidation", "When CI uses stale build output, clear the language cache and make generated artifacts explicit dependencies of the test job."},
	{"cooking", "Overnight sourdough schedule", "Feed the starter in the evening, mix dough after it peaks, bulk ferment slowly overnight and shape the loaf in the morning before baking."},
	{"cooking", "Risotto texture guidance", "Toast rice in butter, add warm stock gradually and stir until starch creates a creamy texture without overcooking the grains."},
	{"cooking", "Pickle brine ratio", "Use vinegar, water, salt and sugar in a balanced brine, then refrigerate vegetables long enough to absorb acidity and aromatics."},
	{"travel", "Kyoto station hotel choice", "For a short Kyoto trip, stay near the train station for luggage convenience and easy rail connections to temples, markets and day trips."},
	{"travel", "Airport layover planning", "During a long layover, check visa rules, luggage storage, transit time and whether public transport remains reliable late at night."},
	{"travel", "Mountain hiking packing list", "Pack rain layers, water, navigation, first aid, warm clothing and enough food when hiking above tree line."},
	{"ops", "Kubernetes log investigation", "When debugging a service in Kubernetes, filter logs by namespace, deployment, pod restart count and request correlation identifiers."},
	{"ops", "Database migration rollback", "Before applying a risky migration, capture a backup, verify rollback SQL and run the migration against a restored staging database."},
	{"ops", "Incident handoff note", "A useful incident handoff includes current impact, mitigation status, owner, timeline, dashboards and the next concrete check."},
	{"frontend", "Dashboard density preference", "Operational dashboards should prioritize compact tables, clear filters, stable controls and readable status indicators over marketing-style sections."},
	{"frontend", "Button accessibility", "Icon buttons need accessible labels and visible focus states so keyboard and screen reader users can operate the interface."},
	{"frontend", "Responsive layout bug", "If card text overlaps at mobile widths, fix container constraints and wrapping instead of scaling type with viewport width."},
	{"docs", "Runbook writing style", "A runbook should list prerequisites, commands, expected output, rollback steps and known failure modes without narrative filler."},
	{"docs", "API changelog entry", "A useful API changelog records the changed endpoint, compatibility impact, migration steps and exact release version."},
	{"docs", "Architecture decision record", "An ADR should capture context, decision, alternatives, consequences and date so future maintainers can understand the tradeoff."},
	{"security", "Secret handling policy", "Never store API keys or credentials in memory. Store durable procedures and references, not raw secrets."},
	{"security", "OAuth callback validation", "Validate redirect URI, state parameter and nonce when handling OAuth callbacks to prevent replay or confused deputy attacks."},
	{"security", "Permission audit checklist", "Review role assignments, stale service accounts, privilege escalation paths and last-used timestamps during an access audit."},
}

var semanticCases = []struct {
	name             string
	query            string
	wantCategory     string
	bannedCategories []string
}{
	{
		name:             "auth paraphrase",
		query:            "users cannot get login verification codes after the company identity provider migration",
		wantCategory:     "auth",
		bannedCategories: []string{"cooking", "travel", "frontend"},
	},
	{
		name:             "finance paraphrase",
		query:            "payment settlement totals are wrong because batch ledger entries use different amounts",
		wantCategory:     "finance",
		bannedCategories: []string{"cooking", "travel", "docs"},
	},
	{
		name:             "testing paraphrase",
		query:            "how do I stop unreliable UI tests from sleeping for arbitrary time",
		wantCategory:     "testing",
		bannedCategories: []string{"finance", "travel", "security"},
	},
	{
		name:             "cooking paraphrase",
		query:            "plan bread dough with a starter so it ferments while I sleep",
		wantCategory:     "cooking",
		bannedCategories: []string{"auth", "ops", "frontend"},
	},
	{
		name:             "travel paraphrase",
		query:            "best area to book lodging in Kyoto when arriving with bags by rail",
		wantCategory:     "travel",
		bannedCategories: []string{"finance", "testing", "security"},
	},
	{
		name:             "security paraphrase",
		query:            "what should we check to avoid leaking credentials into long term memory",
		wantCategory:     "security",
		bannedCategories: []string{"cooking", "travel", "frontend"},
	},
}

func hasCategory(results []SearchResult, category string, limit int) bool {
	for i, result := range results {
		if i >= limit {
			return false
		}
		if semanticCategory(result.Memory.Title) == category {
			return true
		}
	}
	return false
}

func formatSearchResults(results []SearchResult) string {
	var b strings.Builder
	for _, result := range results {
		b.WriteString(semanticCategory(result.Memory.Title))
		b.WriteString(" | ")
		b.WriteString(result.Memory.Title)
		b.WriteString(" | score=")
		b.WriteString(strings.TrimRight(strings.TrimRight(fmtFloat(result.Score), "0"), "."))
		b.WriteByte('\n')
	}
	return b.String()
}

func semanticCategory(title string) string {
	for _, item := range semanticCorpus {
		if item.title == title {
			return item.category
		}
	}
	return ""
}

func fmtFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 4, 64)
}
