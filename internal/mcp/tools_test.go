//go:build sqlite_fts5

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/NathanFirmo/memo/internal/store"
)

func TestToolNamesAreMinimalMemoTools(t *testing.T) {
	want := []string{"memo_add_memory", "memo_remove_memory", "memo_search_memory", "memo_memory_stats"}
	if len(ToolNames) != len(want) {
		t.Fatalf("tool count = %d, want %d", len(ToolNames), len(want))
	}
	for i := range want {
		if ToolNames[i] != want[i] {
			t.Fatalf("ToolNames[%d] = %q, want %q", i, ToolNames[i], want[i])
		}
	}
}

func TestMCPAddSearchAndStats(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(store.OpenOptions{Home: dir, DB: filepath.Join(dir, "memo.db")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	id, err := s.AddMemory(context.Background(), store.MemoryInput{
		Title: "Delete me",
		Body:  "This memory should be removed via MCP.",
	}, nil)
	if err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"memo_add_memory","arguments":{"title":"SQLite memory","body":"Memo stores local memories in SQLite."}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memo_remove_memory","arguments":{"id":` + strconv.FormatInt(id, 10) + `}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"memo_search_memory","arguments":{"query":"SQLite memory"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"memo_memory_stats","arguments":{}}}`,
		"",
	}, "\n")
	var out bytes.Buffer
	if err := Serve(context.Background(), s, &out, strings.NewReader(input)); err != nil {
		t.Fatalf("serve: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("got %d responses: %s", len(lines), out.String())
	}
	for _, line := range lines {
		var resp struct {
			Error any `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		if resp.Error != nil {
			t.Fatalf("unexpected MCP error: %s", line)
		}
	}
	if !strings.Contains(out.String(), "memo_add_memory") {
		t.Fatalf("tools/list missing memo_add_memory: %s", out.String())
	}
	if !strings.Contains(out.String(), "memo_remove_memory") {
		t.Fatalf("tools/list missing memo_remove_memory: %s", out.String())
	}
	if !strings.Contains(out.String(), "SQLite memory") {
		t.Fatalf("search response missing memory: %s", out.String())
	}
	if !strings.Contains(out.String(), "memory removed") {
		t.Fatalf("remove response missing confirmation: %s", out.String())
	}
}
