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

func TestMemoriesMissingEmbeddingsIncludesDirectSQLiteRows(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(OpenOptions{Home: dir, DB: filepath.Join(dir, "memo.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	if _, err := s.db.Exec(`INSERT INTO memories(title, body) VALUES (?, ?)`, "manual memory", "inserted directly through sqlite"); err != nil {
		t.Fatalf("direct insert: %v", err)
	}

	missing, err := s.MemoriesMissingEmbeddings(context.Background(), 0)
	if err != nil {
		t.Fatalf("missing embeddings: %v", err)
	}
	if len(missing) != 1 || missing[0].Title != "manual memory" {
		t.Fatalf("unexpected missing memories: %+v", missing)
	}

	if err := s.UpsertMemoryVector(context.Background(), missing[0].ID, []float32{0.1, 0.2, 0.3}); err != nil {
		t.Fatalf("upsert vector: %v", err)
	}
	missing, err = s.MemoriesMissingEmbeddings(context.Background(), 0)
	if err != nil {
		t.Fatalf("missing embeddings after upsert: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected no missing memories, got %+v", missing)
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

	bestThreshold, report := bestSemanticThreshold(ctx, t, s, embedClient)
	t.Logf("semantic threshold report:\n%s", report)
	if bestThreshold != DefaultMinVectorScore {
		t.Fatalf("DefaultMinVectorScore = %.2f, measured best threshold = %.2f", DefaultMinVectorScore, bestThreshold)
	}

	for _, tc := range semanticCases {
		t.Run(tc.name, func(t *testing.T) {
			queryVec, err := embedClient.Embed(ctx, tc.query)
			if err != nil {
				t.Fatalf("embed query: %v", err)
			}
			results, err := s.Search(ctx, SearchOptions{
				Query:  tc.query,
				Limit:  8,
				Vector: queryVec,
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
			Query:  query,
			Limit:  5,
			Vector: queryVec,
		})
		if err != nil {
			t.Fatalf("search unrelated: %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("unexpected false positives:\n%s", formatSearchResults(results))
		}
	})
}

func bestSemanticThreshold(ctx context.Context, t *testing.T, s *Store, embedClient embed.Client) (float64, string) {
	t.Helper()

	queryVectors := map[string][]float32{}
	for _, tc := range semanticCases {
		vector, err := embedClient.Embed(ctx, tc.query)
		if err != nil {
			t.Fatalf("embed threshold query %q: %v", tc.name, err)
		}
		queryVectors[tc.query] = vector
	}
	unrelatedQuery := "quantum orchid submarine lullaby with marble clouds"
	unrelatedVector, err := embedClient.Embed(ctx, unrelatedQuery)
	if err != nil {
		t.Fatalf("embed unrelated threshold query: %v", err)
	}

	type thresholdResult struct {
		threshold float64
		score     float64
	}
	var results []thresholdResult
	var report strings.Builder
	for _, threshold := range semanticThresholdCandidates {
		score := 0.0
		for _, tc := range semanticCases {
			searchResults, err := s.Search(ctx, SearchOptions{
				Query:          tc.query,
				Limit:          10,
				Vector:         queryVectors[tc.query],
				MinVectorScore: threshold,
			})
			if err != nil {
				t.Fatalf("search threshold %.2f case %q: %v", threshold, tc.name, err)
			}
			if hasCategory(searchResults, tc.wantCategory, 3) {
				score += 4
			} else {
				score -= 8
			}
			for _, banned := range tc.bannedCategories {
				if hasCategory(searchResults, banned, 5) {
					score -= 3
				}
			}
			if len(searchResults) == 0 {
				score -= 3
			}
			if extra := len(searchResults) - 4; extra > 0 {
				score -= float64(extra) * 0.35
			}
		}

		unrelatedResults, err := s.Search(ctx, SearchOptions{
			Query:          unrelatedQuery,
			Limit:          10,
			Vector:         unrelatedVector,
			MinVectorScore: threshold,
		})
		if err != nil {
			t.Fatalf("search unrelated threshold %.2f: %v", threshold, err)
		}
		if len(unrelatedResults) == 0 {
			score += 5
		} else {
			score -= float64(len(unrelatedResults)) * 4
		}

		results = append(results, thresholdResult{threshold: threshold, score: score})
		report.WriteString(fmtFloat(threshold))
		report.WriteString(" score=")
		report.WriteString(fmtFloat(score))
		report.WriteByte('\n')
	}

	best := results[0]
	for _, result := range results[1:] {
		if result.score > best.score || (result.score == best.score && result.threshold > best.threshold) {
			best = result
		}
	}
	return best.threshold, report.String()
}

type semanticMemory struct {
	category string
	title    string
	body     string
}

var semanticThresholdCandidates = []float64{0.58, 0.60, 0.62, 0.64, 0.66, 0.68, 0.70, 0.72, 0.74, 0.76, 0.78, 0.80, 0.82}

var semanticCorpus = []semanticMemory{
	{"auth", "SSO migration 2FA delivery failure", `Context: corporate users were moved from loopback login to the central identity provider. A subset of accounts can still enter email and password, but the second factor code never arrives.

What matters: check login mode, identity provider routing, email aliases, account lock status and fallback authentication before touching account records. The failure is usually delivery or identity routing, not the application session code.

Gotcha: do not solve this by creating duplicate users. Preserve the canonical account and compare migration email, legacy email and current email first.`},
	{"auth", "Keycloak duplicate email remediation", `Context: an identity import can fail because a Keycloak account already owns the email address that the migration wants to write.

Procedure: search by canonical email, legacy email and migration email. Compare the identity provider id against the platform user id before updating. If two people share the same mailbox alias, pause and ask for an owner decision.

Expected memory use: recall this when an agent asks about duplicate email conflicts, migration identity repair or 409 User exists errors.`},
	{"auth", "Passwordless login troubleshooting", `Context: passwordless login sometimes fails after infrastructure or provider changes even though the user exists and has an active account.

Checklist: verify token expiry, one time code delivery, account lock status, callback URL configuration and recent authentication provider changes. If the code is delivered late, look at queue latency before changing user data.

Warning: avoid broad account resets until delivery and callback telemetry are checked.`},
	{"finance", "Payout batch reconciliation mismatch", `Context: payout reconciliation can diverge when one system records the gross payment batch amount while another records the net settled amount.

Debug path: compare payment batch reference, settlement status, ledger movement rows and processor receipt. Work from immutable references first, then compute expected totals from movements.

Gotcha: a batch can look balanced at the payment layer while the ledger is wrong because fee movements or custodian movements were posted at a different lifecycle step.`},
	{"finance", "Wallet transfer balance repair", `Context: internal wallet transfers require paired debit and credit movements. Historical bugs may create only one side or skip the platform ledger call.

Repair approach: recompute account balance from all movements where the account is source or destination. Do not patch the stored balance until the missing transfer rows are identified.

Useful query matches: balance repair, missing transfer, wallet movement, debit credit pair, historical ledger correction.`},
	{"finance", "Invoice capture retry procedure", `Context: card invoice capture failures can be retried only when the payment attempt is idempotent and the processor reference is stable.

Procedure: verify processor reference, authorization code, capture status, settlement timestamp and retry count. If the processor already captured funds, reconcile locally instead of issuing another capture.

Warning: never retry unknown payment state blindly; first classify it as failed, captured, pending or reversed.`},
	{"testing", "React component flaky test", `Context: a React component test around loading state fails intermittently because it asserts too early and sleeps for a fixed duration.

Fix: wait for accessible UI changes, use screen queries that reflect what users see and isolate network mocks per test case. Prefer findByRole or waitForElementToBeRemoved over setTimeout.

Gotcha: if the component has retry behavior, assert on the final stable state, not the transient spinner.`},
	{"testing", "Go integration test database setup", `Context: database tests are reliable when every test owns its own temporary SQLite database and deterministic seed data.

Procedure: create a temp directory, open a fresh database, run schema setup and seed only the records required by the scenario. Avoid shared local state, wall clock assumptions and network calls in unit tests.

Useful when: an agent asks why store tests leak data, why CI passes locally but fails remotely or how to structure database integration tests.`},
	{"testing", "CI cache invalidation", `Context: CI can keep stale build output after generated files, module dependencies or compiler flags change.

Fix: make generated artifacts explicit dependencies of the job and clear language caches when build inputs change. If a task runner says a build is up to date incorrectly, inspect source and generated file declarations.

Gotcha: do not hide cache bugs by always disabling cache; make cache keys represent real inputs.`},
	{"cooking", "Overnight sourdough schedule", `Context: a long sourdough schedule works best when the starter peaks before mixing and bulk fermentation happens slowly overnight.

Plan: feed the starter in the evening, mix dough after it peaks, perform folds early, then let bulk fermentation continue at a cool room temperature. Shape in the morning and bake after proofing.

Warning: if the kitchen is warm, shorten the overnight bulk or use the refrigerator to avoid overproofing.`},
	{"cooking", "Risotto texture guidance", `Context: good risotto depends on starch release and gradual hydration, not cream.

Procedure: toast rice in butter or oil, add warm stock gradually, stir often enough to release starch and stop when grains are tender with a slight bite. Finish with butter and cheese off heat.

Gotcha: adding cold stock repeatedly slows cooking and can make the texture uneven.`},
	{"cooking", "Pickle brine ratio", `Context: refrigerator pickles need a balanced brine and enough time for vegetables to absorb acidity and aromatics.

Baseline: combine vinegar, water, salt and sugar, then add garlic, dill, peppercorns or chile. Pour over sliced vegetables and refrigerate.

Warning: this is for refrigerator pickles, not shelf-stable canning; do not treat it as a preservation safety recipe.`},
	{"travel", "Kyoto station hotel choice", `Context: for a short Kyoto trip with rail arrival and luggage, staying near Kyoto Station reduces friction.

Tradeoff: station hotels make luggage handling, late arrival and day trips easier. Gion or Higashiyama feel more atmospheric but add transit time when carrying bags.

Use this memory for: lodging area, Kyoto itinerary base, train station convenience, short stay travel planning.`},
	{"travel", "Airport layover planning", `Context: whether to leave an airport during a long layover depends on immigration rules, luggage, transit reliability and time of day.

Checklist: verify visa requirements, whether bags are checked through, public transport hours, re-entry security time and the realistic buffer before boarding.

Gotcha: a six hour layover late at night may be worse for city exploration than a shorter daytime layover with reliable trains.`},
	{"travel", "Mountain hiking packing list", `Context: hikes above tree line need more conservative packing because weather changes quickly and navigation can become difficult.

Pack: rain shell, warm layer, water, food, map or offline navigation, first aid, headlamp and emergency communication if coverage is poor.

Warning: do not rely only on a phone battery for navigation in cold or wet conditions.`},
	{"ops", "Kubernetes log investigation", `Context: service debugging in Kubernetes starts by narrowing logs to the namespace, deployment, pod, restart count and request correlation id.

Procedure: check rollout status, recent restarts, previous container logs and events before assuming the application code is wrong. Correlate logs with ingress or gateway request ids.

Useful when: debugging production errors, missing jobs, intermittent 500s or crash loops.`},
	{"ops", "Database migration rollback", `Context: risky migrations need a rollback plan before execution, not after a failure.

Checklist: capture a backup, test restore, verify rollback SQL, run on staging data and record expected row counts. If the migration changes data irreversibly, write a compensating script first.

Gotcha: schema rollback without data rollback can leave the application in a worse state than the original migration failure.`},
	{"ops", "Incident handoff note", `Context: an incident handoff should let the next responder continue without replaying the entire chat.

Include: current impact, mitigation status, owner, timeline, dashboards, logs checked, hypotheses rejected and the next concrete check.

Avoid: vague statements like still investigating without links, timestamps or the next action.`},
	{"frontend", "Dashboard density preference", `Context: operational dashboards are used repeatedly for scanning, comparison and action, not for marketing storytelling.

Design preference: compact tables, clear filters, stable controls, readable status indicators and restrained visual styling. Avoid oversized hero sections, decorative cards and one-note color palettes.

Useful when: building SaaS admin screens, CRM views, internal tools or monitoring surfaces.`},
	{"frontend", "Button accessibility", `Context: icon-only buttons are compact but need accessible labels and visible focus states.

Implementation: provide aria-label or equivalent text, keyboard focus styling, tooltip only as a supplement and sufficient target size. Disabled states should communicate why an action is unavailable when possible.

Gotcha: a tooltip alone is not an accessible name.`},
	{"frontend", "Responsive layout bug", `Context: mobile layouts break when fixed cards, long words or toolbar labels cannot wrap inside their containers.

Fix: define stable dimensions, min and max constraints, wrapping behavior and overflow handling. Do not solve text overlap by scaling font size with viewport width.

Useful query matches: card text overlap, responsive controls, mobile toolbar, layout shift, long labels.`},
	{"docs", "Runbook writing style", `Context: runbooks are operational tools, not essays.

Structure: prerequisites, exact commands, expected output, rollback steps and known failure modes. Keep explanations short and make the next action obvious.

Gotcha: a runbook without verification steps is only a checklist, not a safe operational procedure.`},
	{"docs", "API changelog entry", `Context: API changelogs should help integrators decide whether they need to change code.

Include: endpoint or field changed, compatibility impact, migration steps, release version and examples when the wire shape changes.

Avoid: vague entries like improved API behavior without naming the affected contract.`},
	{"docs", "Architecture decision record", `Context: an ADR should preserve why a technical choice was made.

Write: context, decision, considered alternatives, consequences and date. Keep it short enough that future maintainers will actually read it.

Use when: a decision affects architecture, dependencies, data model or operational constraints.`},
	{"security", "Secret handling policy", `Context: long-term memory must not store API keys, passwords, tokens or raw credentials.

Allowed: durable procedures, warnings, references to where secrets are managed and non-sensitive policy decisions. If a prompt contains a secret, do not save the secret value.

Useful query matches: memory hygiene, credential leak, API key in memory, storing secrets, token handling.`},
	{"security", "OAuth callback validation", `Context: OAuth callback handlers must validate redirect URI, state and nonce to avoid replay and confused deputy attacks.

Checklist: compare the state parameter to the user session, verify nonce when OpenID Connect is used and reject unexpected redirect URIs.

Gotcha: logging full callback URLs can leak authorization codes into logs.`},
	{"security", "Permission audit checklist", `Context: permission audits should focus on stale access and privilege escalation paths.

Review: role assignments, service accounts, last-used timestamps, group inheritance, admin grants and break-glass accounts. Record what changed and who approved it.

Warning: removing access without owner review can break production automation.`},
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
