package registration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/example/splai/pkg/splaiapi"
	"github.com/example/splai/worker/internal/config"
)

func Register(ctx context.Context, cfg config.Config) error {
	backends := inferWorkerBackends(cfg)
	payload := splaiapi.RegisterWorkerRequest{
		WorkerID: cfg.WorkerID,
		CPU:      8,
		Memory:   "16Gi",
		GPU:      false,
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash", "python"},
		Backends: backends,
		Locality: "local",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.ControlPlaneBaseURL, "/")+"/v1/workers/register", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := strings.TrimSpace(cfg.APIToken); tok != "" {
		req.Header.Set("X-SPLAI-Token", tok)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("register worker failed with status %s", resp.Status)
	}
	return nil
}

func inferWorkerBackends(cfg config.Config) []string {
	set := map[string]struct{}{}
	add := func(v string) {
		v = normalizeBackendName(v)
		if v != "" {
			set[v] = struct{}{}
		}
	}
	for _, item := range strings.Split(cfg.WorkerBackends, ",") {
		add(item)
	}
	if strings.TrimSpace(cfg.OllamaBaseURL) != "" {
		add("ollama")
	}
	if strings.TrimSpace(cfg.VLLMBaseURL) != "" {
		add("vllm")
	}
	if strings.TrimSpace(cfg.LlamaCPPBaseURL) != "" {
		add("llama.cpp")
	}
	if strings.TrimSpace(cfg.RemoteAPIBaseURL) != "" {
		add("remote_api")
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func normalizeBackendName(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return ""
	case "remote-api":
		return "remote_api"
	case "llamacpp":
		return "llama.cpp"
	default:
		return strings.ToLower(strings.TrimSpace(v))
	}
}
