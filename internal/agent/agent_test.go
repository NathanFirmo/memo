//go:build sqlite_fts5

package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallerWritesCodexPluginAndInstructions(t *testing.T) {
	home := t.TempDir()
	var stdout bytes.Buffer
	err := Installer{Stdout: &stdout}.Install(context.Background(), InstallOptions{Agent: "codex", Home: home})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	for _, path := range []string{
		filepath.Join(home, "plugins", pluginName, ".codex-plugin", "plugin.json"),
		filepath.Join(home, "plugins", pluginName, ".mcp.json"),
		filepath.Join(home, ".agents", "plugins", "marketplace.json"),
		filepath.Join(home, ".codex", "AGENTS.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected file %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(home, "plugins", pluginName, "hooks"),
		filepath.Join(home, "plugins", pluginName, "scripts"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected no hook path %s, got err %v", path, err)
		}
	}
}
