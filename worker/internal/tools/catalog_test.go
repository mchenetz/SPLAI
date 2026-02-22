package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCatalogMergesBuiltinsAndUserTools(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tools.yaml")
	if err := os.WriteFile(path, []byte(`
version: v1
tools:
  - id: split
    version: "2.0.0"
    command_template: "printf %s {{text}}"
    required_params: ["text"]
    allowed_params: ["text"]
  - id: custom-echo
    version: "1.0.0"
    command_template: "printf %s {{message}}"
    required_params: ["message"]
`), 0o644); err != nil {
		t.Fatalf("write catalog file: %v", err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	split, ok := c.Resolve("split")
	if !ok {
		t.Fatalf("expected overridden split tool")
	}
	if split.Version != "2.0.0" {
		t.Fatalf("expected split override version, got %q", split.Version)
	}
	if _, ok := c.Resolve("custom-echo"); !ok {
		t.Fatalf("expected custom tool")
	}
}

func TestRenderValidatesRequiredAndAllowedParams(t *testing.T) {
	spec := ToolSpec{
		ID:              "echo",
		CommandTemplate: "printf %s {{message}}",
		RequiredParams:  []string{"message"},
		AllowedParams:   []string{"message"},
	}
	cmd, err := spec.Render(map[string]string{"param.message": "hello"})
	if err != nil {
		t.Fatalf("render should succeed: %v", err)
	}
	if !strings.Contains(cmd, "'hello'") {
		t.Fatalf("expected shell-quoted message in command: %s", cmd)
	}
	if _, err := spec.Render(map[string]string{}); err == nil {
		t.Fatalf("expected required param error")
	}
	if _, err := spec.Render(map[string]string{"param.message": "ok", "param.extra": "x"}); err == nil {
		t.Fatalf("expected disallowed param error")
	}
}
