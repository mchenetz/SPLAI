package planner

import "testing"

func TestCompileWithModes(t *testing.T) {
	c := NewCompiler()
	template := c.CompileWithMode("job-1", "batch_analysis", "Analyze 10 tickets", "template")
	llm := c.CompileWithMode("job-2", "batch_analysis", "Analyze 10 tickets", "llm_planner")
	hybrid := c.CompileWithMode("job-3", "batch_analysis", "Analyze 10 tickets", "hybrid")

	if len(template.Tasks) == 0 || template.Tasks[0].TaskID != "t1-split" {
		t.Fatalf("unexpected template plan: %#v", template.Tasks)
	}
	if len(llm.Tasks) == 0 || llm.Tasks[0].TaskID != "t1-decompose" {
		t.Fatalf("unexpected llm plan: %#v", llm.Tasks)
	}
	if len(hybrid.Tasks) <= len(template.Tasks) {
		t.Fatalf("expected hybrid to extend template plan")
	}
	if hybrid.Tasks[len(hybrid.Tasks)-1].Type != "aggregation" {
		t.Fatalf("expected hybrid terminal aggregation task")
	}
}
