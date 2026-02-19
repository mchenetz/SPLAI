package executor

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/example/splai/worker/internal/config"
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
	output := map[string]any{
		"job_id":     t.JobID,
		"task_id":    t.TaskID,
		"type":       t.Type,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
	switch strings.ToLower(strings.TrimSpace(t.Type)) {
	case "llm_inference":
		if parseBool(t.Input["_install_model_if_missing"], false) {
			modelName := firstNonEmpty(t.Input["model"])
			if modelName != "" {
				installedPath, installedNow, err := e.ensureModelInstalled(ctx, modelName, firstNonEmpty(t.Input["model_source"], "huggingface"), true)
				if err != nil {
					return "", fmt.Errorf("install model %s: %w", modelName, err)
				}
				output["model"] = modelName
				output["model_path"] = installedPath
				output["model_installed_now"] = installedNow
			}
		}
		prompt := firstNonEmpty(t.Input["prompt"], t.Input["text"], t.Input["op"])
		output["text"] = "LLM response: " + prompt
	case "model_download":
		modelName := firstNonEmpty(t.Input["model"])
		if modelName == "" {
			return "", errors.New("model_download requires input.model")
		}
		source := firstNonEmpty(t.Input["source"], "huggingface")
		onlyIfMissing := parseBool(t.Input["only_if_missing"], true)
		installedPath, installedNow, err := e.ensureModelInstalled(ctx, modelName, source, onlyIfMissing)
		if err != nil {
			return "", err
		}
		output["model"] = modelName
		output["source"] = source
		output["only_if_missing"] = onlyIfMissing
		output["model_path"] = installedPath
		output["model_installed_now"] = installedNow
		output["result"] = "ok"
	case "tool_execution":
		op := firstNonEmpty(t.Input["op"], "noop")
		output["tool"] = op
		output["result"] = "ok"
	case "embedding":
		seed := firstNonEmpty(t.Input["text"], t.Input["op"], "embedding")
		output["vector"] = deterministicVector(seed, 8)
	case "retrieval":
		output["documents"] = []map[string]any{
			{"id": "doc-1", "score": 0.91, "text": "retrieved context A"},
			{"id": "doc-2", "score": 0.88, "text": "retrieved context B"},
		}
	case "aggregation":
		output["summary"] = aggregateInputs(t.Input)
	default:
		output["result"] = "unsupported task type, defaulted"
	}

	artifactPath := filepath.Join(e.cfg.ArtifactRoot, t.JobID, t.TaskID, "output.json")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(artifactPath, b, 0o644); err != nil {
		return "", err
	}
	if strings.EqualFold(strings.TrimSpace(e.cfg.ArtifactBackend), "minio") {
		if err := e.uploadToMinIO(ctx, artifactPath, t.JobID, t.TaskID); err != nil {
			return "", err
		}
		bucket := strings.TrimSpace(e.cfg.MinIOBucket)
		if bucket == "" {
			bucket = "splai-artifacts"
		}
		return fmt.Sprintf("artifact://s3/%s/%s/%s/output.json", bucket, t.JobID, t.TaskID), nil
	}
	return fmt.Sprintf("artifact://%s/%s/output.json", t.JobID, t.TaskID), nil
}

func (e *Executor) ensureModelInstalled(ctx context.Context, model, source string, onlyIfMissing bool) (string, bool, error) {
	model = strings.TrimSpace(model)
	source = strings.ToLower(strings.TrimSpace(source))
	if model == "" {
		return "", false, errors.New("model is required")
	}
	if source == "" {
		source = "huggingface"
	}
	if source != "huggingface" {
		return "", false, fmt.Errorf("unsupported model source %q", source)
	}
	cacheRoot := strings.TrimSpace(e.cfg.ModelCacheDir)
	if cacheRoot == "" {
		cacheRoot = filepath.Join(e.cfg.ArtifactRoot, "models")
	}
	targetDir := filepath.Join(cacheRoot, filepath.FromSlash(model))
	if dirHasContents(targetDir) && onlyIfMissing {
		return targetDir, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return "", false, err
	}
	if !onlyIfMissing {
		_ = os.RemoveAll(targetDir)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", false, err
	}
	if err := downloadFromHuggingFace(ctx, model, targetDir); err != nil {
		return "", false, err
	}
	return targetDir, true, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func deterministicVector(seed string, n int) []float64 {
	if n <= 0 {
		n = 8
	}
	sum := sha1.Sum([]byte(seed))
	out := make([]float64, 0, n)
	raw := hex.EncodeToString(sum[:])
	for i := 0; i < n; i++ {
		p := raw[(i*2)%len(raw) : ((i*2)%len(raw))+2]
		var b byte
		_, _ = fmt.Sscanf(p, "%02x", &b)
		out = append(out, float64(int(b))/255.0)
	}
	return out
}

func parseBool(v string, fallback bool) bool {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return b
}

func dirHasContents(path string) bool {
	st, err := os.Stat(path)
	if err != nil || !st.IsDir() {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

func downloadFromHuggingFace(ctx context.Context, model, targetDir string) error {
	if _, err := exec.LookPath("hf"); err == nil {
		cmd := exec.CommandContext(ctx, "hf", "download", model, "--local-dir", targetDir)
		if out, runErr := cmd.CombinedOutput(); runErr != nil {
			return fmt.Errorf("hf download failed: %v (%s)", runErr, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if _, err := exec.LookPath("huggingface-cli"); err == nil {
		cmd := exec.CommandContext(ctx, "huggingface-cli", "download", model, "--local-dir", targetDir, "--local-dir-use-symlinks", "False")
		if out, runErr := cmd.CombinedOutput(); runErr != nil {
			return fmt.Errorf("huggingface-cli download failed: %v (%s)", runErr, strings.TrimSpace(string(out)))
		}
		return nil
	}
	repoURL := "https://huggingface.co/" + model
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", repoURL, targetDir)
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("git clone fallback failed: %v (%s)", runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

func aggregateInputs(in map[string]string) string {
	if len(in) == 0 {
		return "no inputs"
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+in[k])
	}
	return strings.Join(parts, "; ")
}

func (e *Executor) uploadToMinIO(ctx context.Context, localPath, jobID, taskID string) error {
	endpoint := strings.TrimSpace(e.cfg.MinIOEndpoint)
	if endpoint == "" {
		return fmt.Errorf("minio endpoint is required when SPLAI_ARTIFACT_BACKEND=minio")
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(e.cfg.MinIOAccessKey, e.cfg.MinIOSecretKey, ""),
		Secure: e.cfg.MinIOUseSSL,
	})
	if err != nil {
		return err
	}
	bucket := strings.TrimSpace(e.cfg.MinIOBucket)
	if bucket == "" {
		bucket = "splai-artifacts"
	}
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return err
		}
	}
	objectName := fmt.Sprintf("%s/%s/output.json", jobID, taskID)
	_, err = client.FPutObject(ctx, bucket, objectName, localPath, minio.PutObjectOptions{ContentType: "application/json"})
	return err
}
