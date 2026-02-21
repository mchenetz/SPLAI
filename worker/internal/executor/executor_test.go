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
	types := []string{"tool_execution", "embedding", "retrieval", "aggregation"}
	for _, typ := range types {
		uri, err := e.Run(context.Background(), Task{
			JobID:  "job-1",
			TaskID: typ,
			Type:   typ,
			Input: map[string]string{
				"prompt":         "hello",
				"op":             "x",
				"documents_json": `[{"id":"d1","text":"hello world"},{"id":"d2","text":"another document"}]`,
				"query":          "hello",
				"items_json":     `[{"summary":"inline aggregation source"}]`,
			},
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
		Type:   "embedding",
		Input:  map[string]string{"text": "hello"},
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

func TestLLMInferenceRequiresConfiguredBackend(t *testing.T) {
	root := t.TempDir()
	e := New(config.Config{ArtifactRoot: root})
	_, err := e.Run(context.Background(), Task{
		JobID:  "job-1",
		TaskID: "t1",
		Type:   "llm_inference",
		Input: map[string]string{
			"prompt":  "hello",
			"backend": "ollama",
		},
	})
	if err == nil {
		t.Fatalf("expected backend configuration error")
	}
	if !strings.Contains(err.Error(), "SPLAI_OLLAMA_BASE_URL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmbeddingBackendsAndBatchInput(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/embed":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model":      "nomic-embed-text",
				"embeddings": [][]float64{{0.1, 0.2}, {0.3, 0.4}},
			})
		case "/v1/embeddings":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "text-embedding-3-small",
				"data": []map[string]any{
					{"embedding": []float64{0.9, 0.8, 0.7}},
					{"embedding": []float64{0.6, 0.5, 0.4}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	root := t.TempDir()
	e := New(config.Config{
		ArtifactRoot:         root,
		OllamaBaseURL:        ts.URL,
		VLLMBaseURL:          ts.URL,
		RemoteAPIBaseURL:     ts.URL,
		RemoteAPIKey:         "k",
		EmbeddingBackend:     "local",
		EmbeddingModel:       "nomic-embed-text",
		EmbeddingDimension:   16,
		EmbeddingHTTPRetries: 1,
	})

	uri, err := e.Run(context.Background(), Task{
		JobID:  "job-embed",
		TaskID: "ollama-batch",
		Type:   "embedding",
		Input: map[string]string{
			"embedding_backend": "ollama",
			"embedding_model":   "nomic-embed-text",
			"texts_json":        `["hello world","another input"]`,
		},
	})
	if err != nil {
		t.Fatalf("ollama embedding failed: %v", err)
	}
	if uri == "" {
		t.Fatalf("expected non-empty uri")
	}

	uri, err = e.Run(context.Background(), Task{
		JobID:  "job-embed",
		TaskID: "vllm-batch",
		Type:   "embedding",
		Input: map[string]string{
			"embedding_backend": "vllm",
			"embedding_model":   "text-embedding-3-small",
			"texts_json":        `["one","two"]`,
		},
	})
	if err != nil {
		t.Fatalf("vllm embedding failed: %v", err)
	}
	if uri == "" {
		t.Fatalf("expected non-empty uri")
	}

	path := filepath.Join(root, "job-embed", "vllm-batch", "output.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read embedding output: %v", err)
	}
	if !strings.Contains(string(b), `"vectors"`) || !strings.Contains(string(b), `"model_version"`) {
		t.Fatalf("expected vectors and model_version metadata in output: %s", string(b))
	}
}

func TestRetrievalLocalHybridWithFilters(t *testing.T) {
	root := t.TempDir()
	e := New(config.Config{
		ArtifactRoot:         root,
		RetrievalBackend:     "local",
		RetrievalHTTPRetries: 1,
	})

	uri, err := e.Run(context.Background(), Task{
		JobID:  "job-retrieval",
		TaskID: "local-filtered",
		Type:   "retrieval",
		Input: map[string]string{
			"query":        "outage in eu west",
			"top_k":        "2",
			"filters_json": `{"tenant":"acme"}`,
			"documents_json": `[
				{"id":"d1","text":"EU west outage report and mitigation","metadata":{"tenant":"acme","source":"tickets"}},
				{"id":"d2","text":"us east latency issue","metadata":{"tenant":"acme","source":"tickets"}},
				{"id":"d3","text":"eu west outage root cause","metadata":{"tenant":"other","source":"tickets"}}
			]`,
		},
	})
	if err != nil {
		t.Fatalf("retrieval run failed: %v", err)
	}
	if uri == "" {
		t.Fatalf("expected non-empty uri")
	}

	path := filepath.Join(root, "job-retrieval", "local-filtered", "output.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	s := string(b)
	if strings.Contains(s, `"id": "d3"`) {
		t.Fatalf("expected tenant filter to exclude d3: %s", s)
	}
	if !strings.Contains(s, `"vector_score"`) || !strings.Contains(s, `"lexical_score"`) {
		t.Fatalf("expected hybrid score fields in output: %s", s)
	}
}

func TestRetrievalRemoteBackend(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/retrieve" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"documents": []map[string]any{
				{"id": "rd1", "score": 0.99, "text": "remote hit"},
			},
		})
	}))
	defer ts.Close()

	root := t.TempDir()
	e := New(config.Config{
		ArtifactRoot:         root,
		RetrievalBackend:     "remote_api",
		RetrievalBaseURL:     ts.URL,
		RetrievalAPIKey:      "k",
		RetrievalHTTPRetries: 1,
	})
	uri, err := e.Run(context.Background(), Task{
		JobID:  "job-retrieval",
		TaskID: "remote",
		Type:   "retrieval",
		Input: map[string]string{
			"query":             "hello",
			"retrieval_backend": "remote_api",
			"documents_json":    `[{"id":"d1","text":"hello world","metadata":{"tenant":"acme"}}]`,
		},
	})
	if err != nil {
		t.Fatalf("remote retrieval run failed: %v", err)
	}
	if uri == "" {
		t.Fatalf("expected non-empty uri")
	}

	path := filepath.Join(root, "job-retrieval", "remote", "output.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(b), `"id": "rd1"`) {
		t.Fatalf("expected remote retrieval result in output: %s", string(b))
	}
}

func TestAggregationReadsDependencyArtifactsWithProvenance(t *testing.T) {
	root := t.TempDir()
	e := New(config.Config{ArtifactRoot: root})

	dep1Path := filepath.Join(root, "job-agg", "t1", "output.json")
	if err := os.MkdirAll(filepath.Dir(dep1Path), 0o755); err != nil {
		t.Fatalf("mkdir dep1: %v", err)
	}
	if err := os.WriteFile(dep1Path, []byte(`{"summary":"ticket clustering complete","root_cause":"network saturation"}`), 0o644); err != nil {
		t.Fatalf("write dep1: %v", err)
	}

	dep2Path := filepath.Join(root, "job-agg", "t2", "output.json")
	if err := os.MkdirAll(filepath.Dir(dep2Path), 0o755); err != nil {
		t.Fatalf("mkdir dep2: %v", err)
	}
	if err := os.WriteFile(dep2Path, []byte(`{"summary":"regional degradation observed","region":"eu-west-1"}`), 0o644); err != nil {
		t.Fatalf("write dep2: %v", err)
	}

	uri, err := e.Run(context.Background(), Task{
		JobID:  "job-agg",
		TaskID: "t3",
		Type:   "aggregation",
		Input: map[string]string{
			"dep:t1:output_uri": "artifact://job-agg/t1/output.json",
			"dep:t2:output_uri": "artifact://job-agg/t2/output.json",
			"required_fields":   "root_cause,region",
		},
	})
	if err != nil {
		t.Fatalf("aggregation run failed: %v", err)
	}
	if uri == "" {
		t.Fatalf("expected non-empty uri")
	}

	outPath := filepath.Join(root, "job-agg", "t3", "output.json")
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"provenance"`) {
		t.Fatalf("expected provenance in aggregation output: %s", s)
	}
	if !strings.Contains(s, `"root_cause": "network saturation"`) || !strings.Contains(s, `"region": "eu-west-1"`) {
		t.Fatalf("expected merged fields from dependency artifacts: %s", s)
	}
}

func TestAggregationRequiredFieldsValidation(t *testing.T) {
	root := t.TempDir()
	e := New(config.Config{ArtifactRoot: root})

	_, err := e.Run(context.Background(), Task{
		JobID:  "job-agg-validate",
		TaskID: "t1",
		Type:   "aggregation",
		Input: map[string]string{
			"items_json":          `[{"summary":"ok"}]`,
			"required_fields":     "root_cause,region",
			"strict_dependencies": "false",
		},
	})
	if err == nil {
		t.Fatalf("expected missing required field validation error")
	}
	if !strings.Contains(err.Error(), "aggregation missing required fields") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestUnsupportedTaskTypeFails(t *testing.T) {
	root := t.TempDir()
	e := New(config.Config{ArtifactRoot: root})
	_, err := e.Run(context.Background(), Task{
		JobID:  "job-1",
		TaskID: "t1",
		Type:   "not_a_real_task",
		Input:  map[string]string{},
	})
	if err == nil {
		t.Fatalf("expected error for unsupported task type")
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

func TestProcessEnvWithPathFallbackHandlesEmptyPath(t *testing.T) {
	t.Setenv("PATH", "")
	env := processEnvWithPathFallback()
	got := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			got = strings.TrimPrefix(kv, "PATH=")
			break
		}
	}
	if got == "" {
		t.Fatalf("expected non-empty PATH fallback")
	}
}
