package tools

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Catalog struct {
	Version string     `yaml:"version" json:"version"`
	Tools   []ToolSpec `yaml:"tools" json:"tools"`

	byID map[string]ToolSpec
}

type ToolSpec struct {
	ID              string   `yaml:"id" json:"id"`
	Version         string   `yaml:"version" json:"version"`
	Description     string   `yaml:"description" json:"description"`
	CommandTemplate string   `yaml:"command_template" json:"command_template"`
	RequiredParams  []string `yaml:"required_params" json:"required_params"`
	AllowedParams   []string `yaml:"allowed_params" json:"allowed_params"`
	TimeoutSeconds  int      `yaml:"timeout_seconds" json:"timeout_seconds"`
}

var placeholderRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

func Load(path string) (Catalog, error) {
	c := Catalog{
		Version: "v1",
		Tools:   builtins(),
	}
	if strings.TrimSpace(path) == "" {
		c.reindex()
		return c, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, err
	}
	var user Catalog
	if err := yaml.Unmarshal(b, &user); err != nil {
		return Catalog{}, err
	}
	if strings.TrimSpace(user.Version) != "" {
		c.Version = strings.TrimSpace(user.Version)
	}
	merged := make(map[string]ToolSpec)
	for _, t := range c.Tools {
		merged[strings.ToLower(strings.TrimSpace(t.ID))] = normalizeSpec(t)
	}
	for _, t := range user.Tools {
		nt := normalizeSpec(t)
		if nt.ID == "" {
			return Catalog{}, fmt.Errorf("tool id is required")
		}
		merged[strings.ToLower(nt.ID)] = nt
	}
	c.Tools = c.Tools[:0]
	ids := make([]string, 0, len(merged))
	for id := range merged {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		c.Tools = append(c.Tools, merged[id])
	}
	c.reindex()
	return c, nil
}

func (c Catalog) Resolve(id string) (ToolSpec, bool) {
	if c.byID == nil {
		return ToolSpec{}, false
	}
	t, ok := c.byID[strings.ToLower(strings.TrimSpace(id))]
	return t, ok
}

func (t ToolSpec) Render(inputs map[string]string) (string, error) {
	if strings.TrimSpace(t.CommandTemplate) == "" {
		return "", fmt.Errorf("tool %q has empty command_template", t.ID)
	}
	params := inputParams(inputs)
	required := toSet(t.RequiredParams)
	allowed := toSet(t.AllowedParams)
	for r := range required {
		if strings.TrimSpace(params[r]) == "" {
			return "", fmt.Errorf("tool %q missing required param %q", t.ID, r)
		}
	}
	if len(allowed) > 0 {
		for k := range params {
			if _, ok := allowed[k]; !ok {
				return "", fmt.Errorf("tool %q disallows param %q", t.ID, k)
			}
		}
	}
	missing := map[string]struct{}{}
	out := placeholderRe.ReplaceAllStringFunc(t.CommandTemplate, func(raw string) string {
		m := placeholderRe.FindStringSubmatch(raw)
		if len(m) < 2 {
			return raw
		}
		key := strings.TrimSpace(m[1])
		v, ok := params[key]
		if !ok || strings.TrimSpace(v) == "" {
			missing[key] = struct{}{}
			return raw
		}
		return shellQuote(v)
	})
	if len(missing) > 0 {
		keys := make([]string, 0, len(missing))
		for k := range missing {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return "", fmt.Errorf("tool %q unresolved params: %s", t.ID, strings.Join(keys, ", "))
	}
	return out, nil
}

func (t ToolSpec) Timeout() int {
	if t.TimeoutSeconds > 0 {
		return t.TimeoutSeconds
	}
	return 30
}

func (c *Catalog) reindex() {
	c.byID = make(map[string]ToolSpec, len(c.Tools))
	for _, t := range c.Tools {
		nt := normalizeSpec(t)
		if nt.ID == "" {
			continue
		}
		c.byID[strings.ToLower(nt.ID)] = nt
	}
}

func normalizeSpec(in ToolSpec) ToolSpec {
	in.ID = strings.TrimSpace(in.ID)
	in.Version = strings.TrimSpace(in.Version)
	in.Description = strings.TrimSpace(in.Description)
	in.CommandTemplate = strings.TrimSpace(in.CommandTemplate)
	in.RequiredParams = trimList(in.RequiredParams)
	in.AllowedParams = trimList(in.AllowedParams)
	return in
}

func trimList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func toSet(in []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" {
			out[v] = struct{}{}
		}
	}
	return out
}

func inputParams(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		key := strings.TrimSpace(k)
		if key == "" || strings.HasPrefix(key, "_") || strings.HasPrefix(key, "dep:") {
			continue
		}
		switch key {
		case "tool", "op", "command", "script", "backend", "model":
			continue
		}
		if strings.HasPrefix(key, "param.") {
			key = strings.TrimPrefix(key, "param.")
		}
		out[key] = strings.TrimSpace(v)
	}
	return out
}

func shellQuote(in string) string {
	return "'" + strings.ReplaceAll(in, "'", `'"'"'`) + "'"
}

func builtins() []ToolSpec {
	return []ToolSpec{
		{
			ID:      "split",
			Version: "1.0.0",
			Description: "Split text into line chunks and emit JSON to stdout for " +
				"downstream parsing.",
			CommandTemplate: "python -c \"import json; t={{text}}; c=[x.strip() for x in str(t).split('\\\\n') if x.strip()]; print(json.dumps({'chunks': c if c else [str(t)]}))\"",
			RequiredParams:  []string{"text"},
			AllowedParams:   []string{"text"},
			TimeoutSeconds:  20,
		},
	}
}
