# Memo

Memo is a tiny local memory tool for AI agents.

It stores notes in one SQLite database, searches them with FTS5, and uses local Ollama embeddings with `sqlite-vec` when available. If Ollama is not running, Memo still works with lexical search.

## Principles

- local-first
- one Go binary
- one SQLite database at `~/.memo/memo.db`
- small CLI
- small MCP server
- no hosted service required

## Install

```bash
task build
sudo install -m 0755 bin/memo /usr/bin/memo
```

Optional semantic search:

```bash
ollama pull mxbai-embed-large
```

## CLI

```bash
memo add --title "Preference" --body "Use concise answers."
memo search "concise answers"
memo remove 1
memo stats
memo doctor
memo embed
memo mcp
memo agent install --agent codex
```

Memo creates the database automatically. Override paths with `MEMO_HOME`, `MEMO_DB_PATH`, `--home`, or `--db`.

## MCP

```json
{
  "mcpServers": {
    "memo": {
      "command": "memo",
      "args": ["mcp"]
    }
  }
}
```

Tools:

- `memo_add_memory`
- `memo_remove_memory`
- `memo_search_memory`
- `memo_memory_stats`

## Agent Install

Memo can install minimal agent memory instructions and MCP config:

```bash
memo agent install --agent codex
memo agent install --agent claude
memo agent uninstall --agent codex
memo agent uninstall --agent claude
```

## Manual SQLite Writes

You can insert memories directly into the SQLite `memories` table. Full-text search is updated by SQLite triggers immediately, but semantic search needs embeddings to be backfilled:

```bash
memo embed
memo embed --limit 100
```
