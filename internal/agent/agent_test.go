//go:build sqlite_fts5

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		filepath.Join(home, ".codex", "config.toml"),
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

func TestInstallerWritesClaudeMCPAndInstructions(t *testing.T) {
	home := t.TempDir()
	var stdout bytes.Buffer
	err := Installer{Stdout: &stdout}.Install(context.Background(), InstallOptions{Agent: "claude", Home: home})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	assertJSONMemoServer(t, filepath.Join(home, ".claude", "mcp.json"))
	assertJSONMemoServer(t, filepath.Join(home, ".claude.json"))
	if _, err := os.Stat(filepath.Join(home, ".claude", "CLAUDE.md")); err != nil {
		t.Fatalf("expected Claude instructions: %v", err)
	}
}

func TestInstallerUninstallRemovesAgentFiles(t *testing.T) {
	home := t.TempDir()
	var stdout bytes.Buffer
	installer := Installer{Stdout: &stdout}
	if err := installer.Install(context.Background(), InstallOptions{Agent: "all", Home: home}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := installer.Uninstall(context.Background(), InstallOptions{Agent: "all", Home: home}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, "plugins", pluginName)); !os.IsNotExist(err) {
		t.Fatalf("expected Codex plugin directory removed, got err %v", err)
	}
	assertNoMemoServer(t, filepath.Join(home, ".claude", "mcp.json"))

	codexConfig, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read codex config: %v", err)
	}
	if strings.Contains(string(codexConfig), "[mcp_servers.memo]") {
		t.Fatalf("expected Codex memo MCP removed:\n%s", codexConfig)
	}
}

func assertJSONMemoServer(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	servers, _ := root["mcpServers"].(map[string]any)
	memo, _ := servers["memo"].(map[string]any)
	if memo["command"] != "memo" {
		t.Fatalf("expected memo command in %s, got %#v", path, memo)
	}
}

func assertNoMemoServer(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if _, ok := servers["memo"]; ok {
		t.Fatalf("expected memo MCP removed from %s", path)
	}
}
