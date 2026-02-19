package planner

import (
	"fmt"
	"strings"
)

type Task struct {
	TaskID       string
	Type         string
	Inputs       map[string]string
	Dependencies []string
	TimeoutSec   int
	MaxRetries   int
}

type DAG struct {
	DAGID string
	Tasks []Task
}

type Compiler struct{}

func NewCompiler() *Compiler {
	return &Compiler{}
}

func (c *Compiler) Compile(jobID, reqType, input string) DAG {
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
