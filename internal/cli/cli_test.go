package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpShowsMinimalCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"help"}, &stdout, &stderr); err != nil {
		t.Fatalf("help: %v", err)
	}
	text := stdout.String()
	for _, want := range []string{"memo add", "memo search", "memo stats", "memo doctor", "memo mcp", "memo agent install"} {
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
