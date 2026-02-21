package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestExecutorLLMBackendAdapters(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"text": "vllm-ok"}},
			})
		case "/completion":
			_ = json.NewEncoder(w).Encode(map[string]any{"content": "llamacpp-ok"})
		case "/api/generate":
			_ = json.NewEncoder(w).Encode(map[string]any{"response": "ollama-ok"})
		case "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"content": "remote-ok"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	root := t.TempDir()
	e := New(config.Config{
		ArtifactRoot:        root,
		OllamaBaseURL:       ts.URL,
		VLLMBaseURL:         ts.URL,
		LlamaCPPBaseURL:     ts.URL,
		RemoteAPIBaseURL:    ts.URL,
		RemoteAPIKey:        "k",
		ArtifactBackend:     "local",
		ModelCacheDir:       filepath.Join(root, "models"),
		MinIOBucket:         "splai-artifacts",
		ControlPlaneBaseURL: "http://localhost:8080",
	})

	cases := []struct {
		name    string
		backend string
	}{
		{name: "ollama", backend: "ollama"},
		{name: "vllm", backend: "vllm"},
		{name: "llama.cpp", backend: "llama.cpp"},
		{name: "remote", backend: "remote_api"},
	}
	for _, tc := range cases {
		uri, err := e.Run(context.Background(), Task{
			JobID:  "job-backend",
			TaskID: tc.name,
			Type:   "llm_inference",
			Input: map[string]string{
				"backend": tc.backend,
				"prompt":  "hello",
				"model":   "test-model",
			},
		})
		if err != nil {
			t.Fatalf("run backend %s: %v", tc.backend, err)
		}
		if uri == "" {
			t.Fatalf("empty uri for backend %s", tc.backend)
		}
	}
}

func TestToolExecutionRunsInSandbox(t *testing.T) {
	root := t.TempDir()
	e := New(config.Config{ArtifactRoot: root})
	_, err := e.Run(context.Background(), Task{
		JobID:  "job-sandbox",
		TaskID: "t1",
		Type:   "tool_execution",
		Input: map[string]string{
			"command": "pwd",
			"op":      "shell",
		},
	})
	if err != nil {
		t.Fatalf("sandbox tool execution failed: %v", err)
	}
	path := filepath.Join(root, "job-sandbox", "t1", "output.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(b), "\"sandboxed\": true") {
		t.Fatalf("expected sandbox marker in output: %s", string(b))
	}
}
