package models

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type RouteInput struct {
	LatencyClass       string
	ReasoningRequired  bool
	DataClassification string
	RequestedModel     string
}

type Decision struct {
	Backend string
	Model   string
	Rule    string
}

type Rule struct {
	Name               string `yaml:"name"`
	WhenLatencyClass   string `yaml:"latency_class"`
	WhenReasoning      *bool  `yaml:"reasoning_required"`
	WhenClassification string `yaml:"data_classification"`
	UseBackend         string `yaml:"use_backend"`
	UseModel           string `yaml:"use_model"`
}

type Config struct {
	DefaultBackend string `yaml:"default_backend"`
	DefaultModel   string `yaml:"default_model"`
	Rules          []Rule `yaml:"rules"`
}

type Router struct {
	cfg Config
}

func NewDefaultRouter() *Router {
	return &Router{
		cfg: Config{
			DefaultBackend: "ollama",
			DefaultModel:   "llama3-8b-q4",
		},
	}
}

func LoadFromEnv() (*Router, error) {
	path := strings.TrimSpace(os.Getenv("SPLAI_MODEL_ROUTING_FILE"))
	if path == "" {
		return NewDefaultRouter(), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read model routing file: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse model routing file: %w", err)
	}
	if strings.TrimSpace(cfg.DefaultBackend) == "" {
		cfg.DefaultBackend = "ollama"
	}
	if strings.TrimSpace(cfg.DefaultModel) == "" {
		cfg.DefaultModel = "llama3-8b-q4"
	}
	return &Router{cfg: cfg}, nil
}

func (r *Router) Route(in RouteInput) Decision {
	decision := Decision{
		Backend: r.cfg.DefaultBackend,
		Model:   r.cfg.DefaultModel,
		Rule:    "default",
	}
	if in.RequestedModel != "" {
		decision.Model = in.RequestedModel
	}
	for _, rule := range r.cfg.Rules {
		if rule.WhenLatencyClass != "" && rule.WhenLatencyClass != in.LatencyClass {
			continue
		}
		if rule.WhenReasoning != nil && *rule.WhenReasoning != in.ReasoningRequired {
			continue
		}
		if rule.WhenClassification != "" && rule.WhenClassification != in.DataClassification {
			continue
		}
		if strings.TrimSpace(rule.UseBackend) != "" {
			decision.Backend = strings.TrimSpace(rule.UseBackend)
		}
		if strings.TrimSpace(rule.UseModel) != "" {
			decision.Model = strings.TrimSpace(rule.UseModel)
		}
		if strings.TrimSpace(rule.Name) != "" {
			decision.Rule = strings.TrimSpace(rule.Name)
		} else {
			decision.Rule = "rule"
		}
		return decision
	}
	return decision
}
