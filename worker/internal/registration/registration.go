package registration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/example/daef/pkg/daefapi"
	"github.com/example/daef/worker/internal/config"
)

func Register(ctx context.Context, cfg config.Config) error {
	payload := daefapi.RegisterWorkerRequest{
		WorkerID: cfg.WorkerID,
		CPU:      8,
		Memory:   "16Gi",
		GPU:      false,
		Models:   []string{"llama3-8b-q4"},
		Tools:    []string{"bash", "python"},
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
