package executor

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
		modelName := firstNonEmpty(t.Input["model"])
		backend := strings.ToLower(firstNonEmpty(t.Input["backend"], "ollama"))
		output["backend"] = backend
		text, err := e.runLLM(ctx, backend, modelName, prompt)
		if err != nil {
			return "", err
		}
		output["text"] = text
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
		output["sandboxed"] = true
		cmd := firstNonEmpty(t.Input["command"], t.Input["script"])
		if cmd == "" {
			output["result"] = "ok"
			break
		}
		stdout, stderr, err := e.runSandboxedCommand(ctx, t.JobID, t.TaskID, cmd)
		output["stdout"] = stdout
		output["stderr"] = stderr
		if err != nil {
			return "", err
		}
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

func (e *Executor) runLLM(ctx context.Context, backend, model, prompt string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "ollama":
		return e.callOllama(ctx, model, prompt)
	case "vllm":
		return e.callVLLM(ctx, model, prompt)
	case "llama.cpp", "llamacpp":
		return e.callLlamaCPP(ctx, model, prompt)
	case "remote", "remote_api", "remote-api":
		return e.callRemoteAPI(ctx, model, prompt)
	default:
		return "LLM response (" + backend + "): " + prompt, nil
	}
}

func (e *Executor) callOllama(ctx context.Context, model, prompt string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(e.cfg.OllamaBaseURL), "/")
	if base == "" {
		return "LLM response (ollama): " + prompt, nil
	}
	body := map[string]any{
		"model":  firstNonEmpty(model, "llama3-8b-q4"),
		"prompt": prompt,
		"stream": false,
	}
	var out struct {
		Response string `json:"response"`
	}
	if err := postJSON(ctx, base+"/api/generate", "", body, &out); err != nil {
		return "", err
	}
	return firstNonEmpty(out.Response, "LLM response (ollama): "+prompt), nil
}

func (e *Executor) callVLLM(ctx context.Context, model, prompt string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(e.cfg.VLLMBaseURL), "/")
	if base == "" {
		return "LLM response (vllm): " + prompt, nil
	}
	body := map[string]any{
		"model":      firstNonEmpty(model, "llama3-8b-q4"),
		"prompt":     prompt,
		"max_tokens": 256,
	}
	var out struct {
		Choices []struct {
			Text string `json:"text"`
		} `json:"choices"`
	}
	if err := postJSON(ctx, base+"/v1/completions", "", body, &out); err != nil {
		return "", err
	}
	if len(out.Choices) > 0 {
		return firstNonEmpty(out.Choices[0].Text, "LLM response (vllm): "+prompt), nil
	}
	return "LLM response (vllm): " + prompt, nil
}

func (e *Executor) callLlamaCPP(ctx context.Context, model, prompt string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(e.cfg.LlamaCPPBaseURL), "/")
	if base == "" {
		return "LLM response (llama.cpp): " + prompt, nil
	}
	body := map[string]any{
		"prompt":      prompt,
		"n_predict":   256,
		"temperature": 0.2,
		"model":       model,
	}
	var out struct {
		Content string `json:"content"`
	}
	if err := postJSON(ctx, base+"/completion", "", body, &out); err != nil {
		return "", err
	}
	return firstNonEmpty(out.Content, "LLM response (llama.cpp): "+prompt), nil
}

func (e *Executor) callRemoteAPI(ctx context.Context, model, prompt string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(e.cfg.RemoteAPIBaseURL), "/")
	if base == "" {
		return "LLM response (remote): " + prompt, nil
	}
	body := map[string]any{
		"model": firstNonEmpty(model, "gpt-4o-mini"),
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"stream": false,
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	auth := strings.TrimSpace(e.cfg.RemoteAPIKey)
	if auth != "" {
		auth = "Bearer " + auth
	}
	if err := postJSON(ctx, base+"/v1/chat/completions", auth, body, &out); err != nil {
		return "", err
	}
	if len(out.Choices) > 0 {
		return firstNonEmpty(out.Choices[0].Message.Content, "LLM response (remote): "+prompt), nil
	}
	return "LLM response (remote): " + prompt, nil
}

func postJSON(ctx context.Context, url, auth string, reqBody any, out any) error {
	b, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("backend request failed: %s %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (e *Executor) runSandboxedCommand(ctx context.Context, jobID, taskID, command string) (string, string, error) {
	sandboxDir := filepath.Join(e.cfg.ArtifactRoot, "sandboxes", jobID, taskID)
	if err := os.MkdirAll(sandboxDir, 0o700); err != nil {
		return "", "", err
	}
	// Lightweight sandbox: isolated working directory, restricted env, and hard timeout.
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "/bin/sh", "-c", command)
	cmd.Dir = sandboxDir
	cmd.Env = []string{
		"PATH=/usr/bin:/bin",
		"HOME=" + sandboxDir,
		"TMPDIR=" + sandboxDir,
	}
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if runCtx.Err() == context.DeadlineExceeded {
		return out.String(), errOut.String(), fmt.Errorf("sandbox command timed out")
	}
	if err != nil {
		return out.String(), errOut.String(), fmt.Errorf("sandbox command failed: %w", err)
	}
	return out.String(), errOut.String(), nil
}
