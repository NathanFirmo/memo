package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	managedStart = "<!-- BEGIN MEMO MEMORY -->"
	managedEnd   = "<!-- END MEMO MEMORY -->"
	pluginName   = "memo-memory"
)

type InstallOptions struct {
	Agent string
	Home  string
}

type Installer struct {
	Stdout io.Writer
}

func (i Installer) Install(ctx context.Context, opts InstallOptions) error {
	home, err := homeDir(opts.Home)
	if err != nil {
		return err
	}
	agent := normalizeAgent(opts.Agent)
	switch agent {
	case "all":
		if err := i.Install(ctx, InstallOptions{Agent: "codex", Home: home}); err != nil {
			return err
		}
		return i.Install(ctx, InstallOptions{Agent: "claude", Home: home})
	case "codex":
		return i.installCodex(home)
	case "claude":
		return i.installClaude(home)
	default:
		return fmt.Errorf("unsupported agent %q; use codex, claude or all", opts.Agent)
	}
}

func (i Installer) Uninstall(ctx context.Context, opts InstallOptions) error {
	home, err := homeDir(opts.Home)
	if err != nil {
		return err
	}
	agent := normalizeAgent(opts.Agent)
	switch agent {
	case "all":
		if err := i.Uninstall(ctx, InstallOptions{Agent: "codex", Home: home}); err != nil {
			return err
		}
		return i.Uninstall(ctx, InstallOptions{Agent: "claude", Home: home})
	case "codex":
		return i.uninstallCodex(home)
	case "claude":
		return i.uninstallClaude(home)
	default:
		return fmt.Errorf("unsupported agent %q; use codex, claude or all", opts.Agent)
	}
}

func (i Installer) installCodex(home string) error {
	pluginPath := filepath.Join(home, "plugins", pluginName)
	files := map[string][]byte{
		filepath.Join(pluginPath, ".codex-plugin", "plugin.json"):   []byte(codexPluginManifest()),
		filepath.Join(pluginPath, ".mcp.json"):                      []byte(mcpConfig()),
		filepath.Join(pluginPath, "skills", pluginName, "SKILL.md"): []byte(skill()),
	}
	for path, data := range files {
		if err := writeFile(path, data, 0o644); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(filepath.Join(pluginPath, "hooks"))
	_ = os.RemoveAll(filepath.Join(pluginPath, "scripts"))
	if err := upsertManagedBlock(filepath.Join(home, ".codex", "AGENTS.md"), Instructions()); err != nil {
		return err
	}
	marketplace, err := ensureCodexMarketplace(filepath.Join(home, ".agents", "plugins", "marketplace.json"))
	if err != nil {
		return err
	}
	if isCurrentUserHome(home) {
		_, err := exec.LookPath("codex")
		if err != nil {
			fmt.Fprintln(i.Stdout, "codex CLI not found; files were written only")
			fmt.Fprintf(i.Stdout, "Codex files installed\nplugin: %s\n", pluginPath)
			return nil
		}
		_ = run(context.Background(), "codex", "plugin", "remove", pluginName+"@"+marketplace)
		_ = run(context.Background(), "codex", "plugin", "add", pluginName+"@"+marketplace)
		_ = run(context.Background(), "codex", "mcp", "remove", "memo")
		_ = run(context.Background(), "codex", "mcp", "add", "memo", "--", "memo", "mcp")
	}
	fmt.Fprintf(i.Stdout, "Codex files installed\nplugin: %s\n", pluginPath)
	return nil
}

func (i Installer) installClaude(home string) error {
	if err := upsertClaudeMCP(filepath.Join(home, ".claude.json")); err != nil {
		return err
	}
	if err := removeClaudeHookCommands(filepath.Join(home, ".claude", "settings.json")); err != nil {
		return err
	}
	if err := upsertManagedBlock(filepath.Join(home, ".claude", "CLAUDE.md"), Instructions()); err != nil {
		return err
	}
	fmt.Fprintln(i.Stdout, "Claude files installed")
	return nil
}

func (i Installer) uninstallCodex(home string) error {
	if isCurrentUserHome(home) {
		if _, err := exec.LookPath("codex"); err == nil {
			_ = run(context.Background(), "codex", "mcp", "remove", "memo")
			_ = run(context.Background(), "codex", "plugin", "remove", pluginName+"@personal")
		}
	}
	if err := removeManagedBlock(filepath.Join(home, ".codex", "AGENTS.md")); err != nil {
		return err
	}
	fmt.Fprintln(i.Stdout, "Codex Memo instructions removed")
	return nil
}

func (i Installer) uninstallClaude(home string) error {
	if err := removeClaudeHookCommands(filepath.Join(home, ".claude", "settings.json")); err != nil {
		return err
	}
	if err := removeManagedBlock(filepath.Join(home, ".claude", "CLAUDE.md")); err != nil {
		return err
	}
	fmt.Fprintln(i.Stdout, "Claude Memo instructions removed")
	return nil
}

func Instructions() string {
	return fmt.Sprintf(`%s

## Memo Memory

Memo is the user's local long-term memory. Search Memo for relevant context when useful. Save only durable preferences, decisions, procedures, project conventions, warnings and important facts.

Use memory hygienically. Do not save secrets, raw transcript dumps, command output, generic final answers or short-lived implementation chatter.

MCP tools:

- memo_add_memory
- memo_search_memory
- memo_memory_stats
%s`, managedStart, managedEnd)
}

func normalizeAgent(agent string) string {
	agent = strings.ToLower(strings.TrimSpace(agent))
	if agent == "" {
		return "all"
	}
	return agent
}

func homeDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return os.UserHomeDir()
}

func isCurrentUserHome(path string) bool {
	home, err := os.UserHomeDir()
	return err == nil && filepath.Clean(path) == filepath.Clean(home)
}

func writeFile(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}

func upsertManagedBlock(path, block string) error {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return writeFile(path, []byte(block+"\n"), 0o644)
	}
	if err != nil {
		return err
	}
	text := string(content)
	start := strings.Index(text, managedStart)
	end := strings.Index(text, managedEnd)
	if start >= 0 && end >= start {
		end += len(managedEnd)
		text = strings.TrimSpace(text[:start]) + "\n\n" + block + "\n\n" + strings.TrimSpace(text[end:])
	} else {
		text = strings.TrimRight(text, "\n") + "\n\n" + block + "\n"
	}
	return writeFile(path, []byte(strings.TrimSpace(text)+"\n"), 0o644)
}

func removeManagedBlock(path string) error {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	text := string(content)
	start := strings.Index(text, managedStart)
	end := strings.Index(text, managedEnd)
	if start < 0 || end < start {
		return nil
	}
	end += len(managedEnd)
	next := strings.TrimSpace(text[:start]) + "\n\n" + strings.TrimSpace(text[end:])
	return writeFile(path, []byte(strings.TrimSpace(next)+"\n"), 0o644)
}

func upsertClaudeMCP(path string) error {
	root := map[string]any{}
	_ = readJSON(path, &root)
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		root["mcpServers"] = servers
	}
	servers["memo"] = map[string]any{"type": "stdio", "command": "memo", "args": []any{"mcp"}}
	return writeJSON(path, root)
}

func removeClaudeHookCommands(path string) error {
	root := map[string]any{}
	if err := readJSON(path, &root); err != nil {
		return err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	for _, event := range []string{"UserPromptSubmit", "PostToolUse"} {
		current, _ := hooks[event].([]any)
		var next []any
		for _, item := range current {
			entry, _ := item.(map[string]any)
			hookItems, _ := entry["hooks"].([]any)
			var kept []any
			for _, hookItem := range hookItems {
				hook, _ := hookItem.(map[string]any)
				command, _ := hook["command"].(string)
				if strings.Contains(command, "memo agent hook") {
					continue
				}
				kept = append(kept, hookItem)
			}
			if len(kept) == 0 {
				continue
			}
			entry["hooks"] = kept
			next = append(next, entry)
		}
		if len(next) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = next
		}
	}
	if len(hooks) == 0 {
		delete(root, "hooks")
	}
	return writeJSON(path, root)
}

func readJSON(path string, dst *map[string]any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, dst)
}

func writeJSON(path string, value map[string]any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(data, '\n'), 0o644)
}

func ensureCodexMarketplace(path string) (string, error) {
	root := map[string]any{}
	_ = readJSON(path, &root)
	if root["name"] == nil {
		root["name"] = "personal"
	}
	if root["interface"] == nil {
		root["interface"] = map[string]any{"displayName": "Personal"}
	}
	plugins, _ := root["plugins"].([]any)
	entry := map[string]any{
		"name": pluginName,
		"source": map[string]any{
			"source": "local",
			"path":   "./plugins/" + pluginName,
		},
		"policy": map[string]any{
			"installation":   "AVAILABLE",
			"authentication": "ON_INSTALL",
		},
		"category": "Productivity",
	}
	replaced := false
	for index, item := range plugins {
		plugin, _ := item.(map[string]any)
		if plugin["name"] == pluginName {
			plugins[index] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		plugins = append(plugins, entry)
	}
	root["plugins"] = plugins
	if err := writeJSON(path, root); err != nil {
		return "", err
	}
	name, _ := root["name"].(string)
	if strings.TrimSpace(name) == "" {
		name = "personal"
	}
	return name, nil
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, msg)
		}
		return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func codexPluginManifest() string {
	return `{
  "name": "memo-memory",
  "version": "0.1.0",
  "description": "Local Memo memory for Codex.",
  "license": "MIT",
  "skills": "./skills/",
  "mcpServers": "./.mcp.json"
}
`
}

func mcpConfig() string {
	return `{
  "mcpServers": {
    "memo": {
      "command": "memo",
      "args": ["mcp"]
    }
  }
}
`
}

func skill() string {
	return `---
name: memo-memory
description: Use Memo local memory for durable preferences, decisions, procedures, warnings and project conventions.
---

# Memo Memory

Use Memo as local long-term memory. Search it when useful and save only durable context.
`
}
