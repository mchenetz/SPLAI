package executor

import (
	"context"
	"fmt"

	"github.com/example/daef/worker/internal/config"
)

type Executor struct {
	cfg config.Config
}

func New(cfg config.Config) *Executor {
	return &Executor{cfg: cfg}
}

type Task struct {
	JobID  string
	TaskID string
	Type   string
	Input  map[string]string
}

func (e *Executor) Run(ctx context.Context, t Task) (string, error) {
	_ = ctx
	return fmt.Sprintf("artifact://%s/%s/output.json", t.JobID, t.TaskID), nil
}
