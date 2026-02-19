package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/splai/worker/internal/config"
)

func TestExecutorWritesArtifactsForTaskTypes(t *testing.T) {
	root := t.TempDir()
	e := New(config.Config{ArtifactRoot: root})
	types := []string{"llm_inference", "tool_execution", "embedding", "retrieval", "aggregation"}
	for _, typ := range types {
		uri, err := e.Run(context.Background(), Task{
			JobID:  "job-1",
			TaskID: typ,
			Type:   typ,
			Input:  map[string]string{"prompt": "hello", "op": "x"},
		})
		if err != nil {
			t.Fatalf("run type %s: %v", typ, err)
		}
		if uri == "" {
			t.Fatalf("empty artifact uri for %s", typ)
		}
		path := filepath.Join(root, "job-1", typ, "output.json")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("artifact missing for %s: %v", typ, err)
		}
	}
}

func TestExecutorMinioBackendRequiresEndpoint(t *testing.T) {
	root := t.TempDir()
	e := New(config.Config{
		ArtifactRoot:    root,
		ArtifactBackend: "minio",
	})
	_, err := e.Run(context.Background(), Task{
		JobID:  "job-1",
		TaskID: "t1",
		Type:   "llm_inference",
		Input:  map[string]string{"prompt": "hello"},
	})
	if err == nil {
		t.Fatalf("expected error for missing minio endpoint")
	}
	if !strings.Contains(err.Error(), "minio endpoint") {
		t.Fatalf("unexpected error: %v", err)
	}
}
