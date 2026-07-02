# Repository Guidelines

## Project Structure & Module Organization

Memo is a small Go CLI and MCP server. The entry point lives in `cmd/memo/main.go`. Package code is under `internal/`: `cli` handles commands, `mcp` exposes MCP tools, `store` owns SQLite persistence, `memory` contains shared memory types, `embed` integrates Ollama embeddings, `config` handles paths and environment, and `agent` installs agent configuration. SQL schema files are in `internal/store/schema.sql` and versioned migrations are in `migrations/`.

Tests sit next to the packages they cover using Go's standard `*_test.go` pattern, such as `internal/store/store_test.go`.

## Build, Test, and Development Commands

- `task` lists available Taskfile targets.
- `task build` builds the CLI to `bin/memo` with the `sqlite_fts5` build tag.
- `task test` runs `go test -tags "sqlite_fts5" ./...`.
- `task clean` removes local build artifacts from `bin/`.
- `go run -tags "sqlite_fts5" ./cmd/memo doctor` is useful for checking local configuration while developing.

Optional semantic search depends on a local Ollama model, for example `ollama pull mxbai-embed-large`. Memo still supports lexical search without Ollama.

## Coding Style & Naming Conventions

Use standard Go formatting: run `gofmt` on changed Go files before committing. Keep package names short, lowercase, and aligned with directory names. Prefer clear exported names only for cross-package APIs; keep helpers unexported when package-local.

Follow the existing error-handling style: return contextual errors rather than panicking, and keep CLI-facing text concise. Keep SQL changes in schema or migration files, not embedded ad hoc strings unless package-local behavior requires it.

## Testing Guidelines

Use Go's built-in `testing` package. Name tests by behavior, for example `TestSearchReturnsLexicalMatches`. Add or update tests near the package being changed. For storage behavior, cover both successful operations and persistence/search edge cases. Always run `task test` before opening a pull request.

## Commit & Pull Request Guidelines

The repository history uses Conventional Commits, such as `feat: initialize memo project` and `feat: add manual memory embedding workflow`. Continue using `type: summary` in the imperative mood, for example `fix: handle missing memo home`.

Pull requests should include a short description, the reason for the change, test results, and any migration or configuration impact. Link related issues when available. Include CLI examples or screenshots only when user-facing behavior changes.

## Security & Configuration Tips

Memo stores local data in SQLite, defaulting to `~/.memo/memo.db`. Do not commit local databases, generated binaries, secrets, or machine-specific agent configuration. Prefer `MEMO_HOME`, `MEMO_DB_PATH`, `--home`, or `--db` for isolated test data.
