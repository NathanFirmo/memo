package config

import (
	"path/filepath"
	"testing"
)

func TestResolveDefaultsToMemoHome(t *testing.T) {
	t.Setenv("MEMO_HOME", "")
	t.Setenv("MEMO_DB_PATH", "")

	paths, err := Resolve("", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if filepath.Base(paths.Home) != ".memo" {
		t.Fatalf("home = %q, want .memo suffix", paths.Home)
	}
	if filepath.Base(paths.DB) != "memo.db" {
		t.Fatalf("db = %q, want memo.db suffix", paths.DB)
	}
}

func TestResolveEnvOverrides(t *testing.T) {
	t.Setenv("MEMO_HOME", "/tmp/custom-memo")
	t.Setenv("MEMO_DB_PATH", "/tmp/custom-memo/db.sqlite")

	paths, err := Resolve("", "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if paths.Home != "/tmp/custom-memo" {
		t.Fatalf("home = %q", paths.Home)
	}
	if paths.DB != "/tmp/custom-memo/db.sqlite" {
		t.Fatalf("db = %q", paths.DB)
	}
}
