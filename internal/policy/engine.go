package policy

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type TenantQuota struct {
	MaxRunningJobs  int `yaml:"max_running_jobs"`
	MaxRunningTasks int `yaml:"max_running_tasks"`
}

type RuleMatch struct {
	Tenant             string `yaml:"tenant"`
	JobType            string `yaml:"job_type"`
	TaskType           string `yaml:"task_type"`
	Model              string `yaml:"model"`
	DataClassification string `yaml:"data_classification"`
	Priority           string `yaml:"priority"`
	NetworkIsolation   string `yaml:"network_isolation"`
	WorkerLocality     string `yaml:"worker_locality"`
	RequiresGPU        *bool  `yaml:"requires_gpu"`
}

type Rule struct {
	Name   string    `yaml:"name"`
	Effect string    `yaml:"effect"` // allow|deny
	Reason string    `yaml:"reason"`
	Match  RuleMatch `yaml:"match"`
}

type Config struct {
	DefaultAction string                 `yaml:"default_action"` // allow|deny
	Rules         []Rule                 `yaml:"rules"`
	TenantQuotas  map[string]TenantQuota `yaml:"tenant_quotas"`
}

type Decision struct {
	Allowed    bool
	ReasonCode string
	Rule       string
	Message    string
}

type SubmitInput struct {
	Tenant             string
	JobType            string
	Priority           string
	Model              string
	DataClassification string
	RunningJobs        int
}

type AssignmentInput struct {
	Tenant             string
	TaskType           string
	Model              string
	DataClassification string
	NetworkIsolation   string
	WorkerLocality     string
	WorkerGPU          bool
	RunningTasks       int
}

type Engine struct {
	defaultAction string
	rules         []Rule
	quotas        map[string]TenantQuota
	noop          bool
}

func NewAllowAll() *Engine {
	return &Engine{
		defaultAction: "allow",
		rules:         nil,
		quotas:        map[string]TenantQuota{},
		noop:          true,
	}
}

func LoadFromEnv() (*Engine, error) {
	path := strings.TrimSpace(os.Getenv("SPLAI_POLICY_FILE"))
	if path == "" {
		return NewAllowAll(), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy file: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse policy file: %w", err)
	}
	return NewFromConfig(cfg), nil
}

func NewFromConfig(cfg Config) *Engine {
	e := &Engine{
		defaultAction: normalizeAction(cfg.DefaultAction),
		rules:         make([]Rule, 0, len(cfg.Rules)),
		quotas:        map[string]TenantQuota{},
	}
	for _, r := range cfg.Rules {
		r.Effect = normalizeAction(r.Effect)
		if r.Effect == "" {
			r.Effect = "deny"
		}
		e.rules = append(e.rules, r)
	}
	for k, v := range cfg.TenantQuotas {
		e.quotas[strings.TrimSpace(k)] = v
	}
	if e.defaultAction == "" {
		e.defaultAction = "allow"
	}
	if e.defaultAction == "allow" && len(e.rules) == 0 && len(e.quotas) == 0 {
		e.noop = true
	}
	return e
}

func (e *Engine) IsNoop() bool { return e != nil && e.noop }

func (e *Engine) EvaluateSubmit(in SubmitInput) Decision {
	tenant := strings.TrimSpace(in.Tenant)
	if tenant == "" {
		tenant = "default"
	}
	if q, ok := e.quotas[tenant]; ok && q.MaxRunningJobs > 0 && in.RunningJobs >= q.MaxRunningJobs {
		return Decision{
			Allowed:    false,
			ReasonCode: "quota_running_jobs_exceeded",
			Rule:       "tenant_quotas." + tenant,
			Message:    fmt.Sprintf("running jobs %d reached max_running_jobs %d", in.RunningJobs, q.MaxRunningJobs),
		}
	}
	return e.evaluateRules(RuleMatch{
		Tenant:             tenant,
		JobType:            in.JobType,
		Model:              in.Model,
		DataClassification: in.DataClassification,
		Priority:           in.Priority,
	})
}

func (e *Engine) EvaluateAssignment(in AssignmentInput) Decision {
	tenant := strings.TrimSpace(in.Tenant)
	if tenant == "" {
		tenant = "default"
	}
	if q, ok := e.quotas[tenant]; ok && q.MaxRunningTasks > 0 && in.RunningTasks >= q.MaxRunningTasks {
		return Decision{
			Allowed:    false,
			ReasonCode: "quota_running_tasks_exceeded",
			Rule:       "tenant_quotas." + tenant,
			Message:    fmt.Sprintf("running tasks %d reached max_running_tasks %d", in.RunningTasks, q.MaxRunningTasks),
		}
	}
	return e.evaluateRules(RuleMatch{
		Tenant:             tenant,
		TaskType:           in.TaskType,
		Model:              in.Model,
		DataClassification: in.DataClassification,
		NetworkIsolation:   in.NetworkIsolation,
		WorkerLocality:     in.WorkerLocality,
		RequiresGPU:        &in.WorkerGPU,
	})
}

func (e *Engine) evaluateRules(input RuleMatch) Decision {
	for _, r := range e.rules {
		if !matches(r.Match, input) {
			continue
		}
		allowed := r.Effect == "allow"
		reason := "policy_rule_" + r.Effect
		if r.Reason != "" {
			reason = strings.TrimSpace(r.Reason)
		}
		msg := reason
		if r.Name != "" {
			msg = r.Name + ": " + reason
		}
		return Decision{
			Allowed:    allowed,
			ReasonCode: reason,
			Rule:       r.Name,
			Message:    msg,
		}
	}
	if e.defaultAction == "deny" {
		return Decision{
			Allowed:    false,
			ReasonCode: "default_deny",
			Rule:       "default_action",
			Message:    "request denied by default_action=deny",
		}
	}
	return Decision{
		Allowed:    true,
		ReasonCode: "default_allow",
		Rule:       "default_action",
		Message:    "request allowed by default_action=allow",
	}
}

func matches(rule RuleMatch, in RuleMatch) bool {
	if rule.Tenant != "" && rule.Tenant != in.Tenant {
		return false
	}
	if rule.JobType != "" && rule.JobType != in.JobType {
		return false
	}
	if rule.TaskType != "" && rule.TaskType != in.TaskType {
		return false
	}
	if rule.Model != "" && rule.Model != in.Model {
		return false
	}
	if rule.DataClassification != "" && rule.DataClassification != in.DataClassification {
		return false
	}
	if rule.Priority != "" && rule.Priority != in.Priority {
		return false
	}
	if rule.NetworkIsolation != "" && rule.NetworkIsolation != in.NetworkIsolation {
		return false
	}
	if rule.WorkerLocality != "" && rule.WorkerLocality != in.WorkerLocality {
		return false
	}
	if rule.RequiresGPU != nil && *rule.RequiresGPU != derefBool(in.RequiresGPU) {
		return false
	}
	return true
}

func normalizeAction(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "allow":
		return "allow"
	case "deny":
		return "deny"
	default:
		return ""
	}
}

func derefBool(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}
