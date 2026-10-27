package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/NathanFirmo/memo/internal/store"
)

func TestHelpShowsMinimalCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"help"}, &stdout, &stderr); err != nil {
		t.Fatalf("help: %v", err)
	}
	text := stdout.String()
	for _, want := range []string{"memo add", "memo search", "memo remove", "memo stats", "memo doctor", "memo embed", "memo mcp", "memo agent install", "memo agent uninstall"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %q:\n%s", want, text)
		}
	}
	for _, removed := range []string{"memo associate", "memo consolidate", "memo reinforce", "memo inhibit"} {
		if strings.Contains(text, removed) {
			t.Fatalf("help still mentions removed command %q:\n%s", removed, text)
		}
	}
}

func TestRemoveDeletesMemoryByID(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "memo.db")
	s, err := store.Open(store.OpenOptions{Home: dir, DB: db})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	id, err := s.AddMemory(context.Background(), store.MemoryInput{
		Title: "Temporary memory",
		Body:  "Remove through the CLI.",
	}, nil)
	if err != nil {
		t.Fatalf("add memory: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := Run([]string{"remove", "--home", dir, "--db", db, strconv.FormatInt(id, 10)}, &stdout, &stderr); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(stdout.String(), "memory removed") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}

	s, err = store.Open(store.OpenOptions{Home: dir, DB: db})
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer s.Close()
	stats, err := s.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Memories != 0 {
		t.Fatalf("memory was not removed: %+v", stats)
	}
}
