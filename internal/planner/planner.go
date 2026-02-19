package planner

import (
	"fmt"
	"log"
	"os"
	"strings"
)

type Task struct {
	TaskID       string            `json:"task_id"`
	Type         string            `json:"type"`
	Inputs       map[string]string `json:"inputs"`
	Dependencies []string          `json:"dependencies,omitempty"`
	TimeoutSec   int               `json:"timeout_sec,omitempty"`
	MaxRetries   int               `json:"max_retries,omitempty"`
}

type DAG struct {
	DAGID string `json:"dag_id"`
	Tasks []Task `json:"tasks"`
}

func NewCompiler() *Compiler {
	var llm LLMProvider
	if endpoint := strings.TrimSpace(os.Getenv("SPLAI_LLM_PLANNER_ENDPOINT")); endpoint != "" {
		llm = NewHTTPProvider(endpoint, strings.TrimSpace(os.Getenv("SPLAI_LLM_PLANNER_API_KEY")))
	}
	return &Compiler{llm: llm}
}

func NewCompilerWithLLM(provider LLMProvider) *Compiler {
	return &Compiler{llm: provider}
}

type Compiler struct {
	llm LLMProvider
}

func (c *Compiler) Compile(jobID, reqType, input string) DAG {
	return c.CompileWithMode(jobID, reqType, input, "hybrid")
}

func (c *Compiler) CompileWithMode(jobID, reqType, input, mode string) DAG {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "hybrid":
		return c.compileHybrid(jobID, reqType, input)
	case "template":
		return c.compileTemplate(jobID, reqType, input)
	case "llm", "llm_planner":
		return c.compileLLM(jobID, reqType, input)
	default:
		return c.compileHybrid(jobID, reqType, input)
	}
}

func (c *Compiler) compileTemplate(jobID, reqType, input string) DAG {
	if strings.Contains(strings.ToLower(input), "analyze") || reqType == "batch_analysis" {
		return DAG{
			DAGID: fmt.Sprintf("%s-dag", jobID),
			Tasks: []Task{
				{TaskID: "t1-split", Type: "tool_execution", Inputs: map[string]string{"op": "split", "text": input}, TimeoutSec: 30, MaxRetries: 2},
				{TaskID: "t2-embed", Type: "embedding", Inputs: map[string]string{"op": "embed"}, Dependencies: []string{"t1-split"}, TimeoutSec: 60, MaxRetries: 2},
				{TaskID: "t3-summarize", Type: "llm_inference", Inputs: map[string]string{"op": "summarize"}, Dependencies: []string{"t2-embed"}, TimeoutSec: 60, MaxRetries: 2},
			},
		}
	}

	return DAG{
		DAGID: fmt.Sprintf("%s-dag", jobID),
		Tasks: []Task{
			{TaskID: "t1", Type: "llm_inference", Inputs: map[string]string{"prompt": input}, TimeoutSec: 60, MaxRetries: 2},
		},
	}
}

func (c *Compiler) compileLLM(jobID, reqType, input string) DAG {
	if c.llm != nil {
		if dag, err := c.llm.Plan(jobID, reqType, input); err == nil && len(dag.Tasks) > 0 {
			return dag
		} else if err != nil {
			log.Printf("llm planner provider failed, falling back to static decomposition: %v", err)
		}
	}
	if strings.Contains(strings.ToLower(input), "analyze") || reqType == "batch_analysis" {
		return DAG{
			DAGID: fmt.Sprintf("%s-dag", jobID),
			Tasks: []Task{
				{TaskID: "t1-decompose", Type: "llm_inference", Inputs: map[string]string{"op": "decompose", "prompt": input}, TimeoutSec: 45, MaxRetries: 2},
				{TaskID: "t2-retrieve", Type: "retrieval", Inputs: map[string]string{"op": "retrieve"}, Dependencies: []string{"t1-decompose"}, TimeoutSec: 45, MaxRetries: 2},
				{TaskID: "t3-reason", Type: "llm_inference", Inputs: map[string]string{"op": "reason"}, Dependencies: []string{"t2-retrieve"}, TimeoutSec: 90, MaxRetries: 2},
				{TaskID: "t4-report", Type: "aggregation", Inputs: map[string]string{"op": "report"}, Dependencies: []string{"t3-reason"}, TimeoutSec: 45, MaxRetries: 2},
			},
		}
	}
	return DAG{
		DAGID: fmt.Sprintf("%s-dag", jobID),
		Tasks: []Task{
			{TaskID: "t1-decompose", Type: "llm_inference", Inputs: map[string]string{"op": "decompose", "prompt": input}, TimeoutSec: 45, MaxRetries: 2},
			{TaskID: "t2-respond", Type: "llm_inference", Inputs: map[string]string{"op": "respond"}, Dependencies: []string{"t1-decompose"}, TimeoutSec: 60, MaxRetries: 2},
		},
	}
}

func (c *Compiler) compileHybrid(jobID, reqType, input string) DAG {
	base := c.compileTemplate(jobID, reqType, input)
	if len(base.Tasks) <= 1 {
		return base
	}
	// Hybrid mode adds an explicit aggregation tail to template decomposition.
	base.Tasks = append(base.Tasks, Task{
		TaskID:       "t4-aggregate",
		Type:         "aggregation",
		Inputs:       map[string]string{"op": "aggregate"},
		Dependencies: []string{base.Tasks[len(base.Tasks)-1].TaskID},
		TimeoutSec:   30,
		MaxRetries:   2,
	})
	return base
}
