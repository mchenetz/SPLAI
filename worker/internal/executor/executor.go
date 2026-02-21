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
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

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
		text := firstNonEmpty(t.Input["text"], t.Input["prompt"], t.Input["op"])
		if text == "" {
			return "", errors.New("embedding requires non-empty text/prompt")
		}
		output["vector"] = embedText(text, 128)
		output["dimension"] = 128
	case "retrieval":
		query := firstNonEmpty(t.Input["query"], t.Input["text"], t.Input["prompt"])
		docs, err := decodeDocuments(t.Input)
		if err != nil {
			return "", err
		}
		hits := retrieveDocuments(query, docs, 5)
		output["documents"] = hits
		output["query"] = query
	case "aggregation":
		output["summary"] = aggregateInputs(t.Input)
	default:
		return "", fmt.Errorf("unsupported task type %q", t.Type)
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
		return "", fmt.Errorf("unsupported llm backend %q", backend)
	}
}

func (e *Executor) callOllama(ctx context.Context, model, prompt string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(e.cfg.OllamaBaseURL), "/")
	if base == "" {
		return "", errors.New("SPLAI_OLLAMA_BASE_URL is required for backend=ollama")
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
	if strings.TrimSpace(out.Response) == "" {
		return "", errors.New("ollama returned empty response")
	}
	return strings.TrimSpace(out.Response), nil
}

func (e *Executor) callVLLM(ctx context.Context, model, prompt string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(e.cfg.VLLMBaseURL), "/")
	if base == "" {
		return "", errors.New("SPLAI_VLLM_BASE_URL is required for backend=vllm")
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
		txt := strings.TrimSpace(out.Choices[0].Text)
		if txt != "" {
			return txt, nil
		}
	}
	return "", errors.New("vllm returned empty choices")
}

func (e *Executor) callLlamaCPP(ctx context.Context, model, prompt string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(e.cfg.LlamaCPPBaseURL), "/")
	if base == "" {
		return "", errors.New("SPLAI_LLAMACPP_BASE_URL is required for backend=llama.cpp")
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
	if strings.TrimSpace(out.Content) == "" {
		return "", errors.New("llama.cpp returned empty content")
	}
	return strings.TrimSpace(out.Content), nil
}

func (e *Executor) callRemoteAPI(ctx context.Context, model, prompt string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(e.cfg.RemoteAPIBaseURL), "/")
	if base == "" {
		return "", errors.New("SPLAI_REMOTE_API_BASE_URL is required for backend=remote_api")
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
		txt := strings.TrimSpace(out.Choices[0].Message.Content)
		if txt != "" {
			return txt, nil
		}
	}
	return "", errors.New("remote api returned empty choices")
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

type retrievalDoc struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func decodeDocuments(in map[string]string) ([]retrievalDoc, error) {
	if raw := strings.TrimSpace(in["documents_json"]); raw != "" {
		var docs []retrievalDoc
		if err := json.Unmarshal([]byte(raw), &docs); err != nil {
			return nil, fmt.Errorf("invalid documents_json: %w", err)
		}
		out := make([]retrievalDoc, 0, len(docs))
		for i, d := range docs {
			if strings.TrimSpace(d.Text) == "" {
				continue
			}
			if strings.TrimSpace(d.ID) == "" {
				d.ID = fmt.Sprintf("doc-%d", i+1)
			}
			out = append(out, d)
		}
		return out, nil
	}
	if raw := strings.TrimSpace(in["documents"]); raw != "" {
		lines := strings.Split(raw, "\n")
		out := make([]retrievalDoc, 0, len(lines))
		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			out = append(out, retrievalDoc{ID: fmt.Sprintf("doc-%d", i+1), Text: line})
		}
		return out, nil
	}
	return nil, nil
}

func retrieveDocuments(query string, docs []retrievalDoc, topK int) []map[string]any {
	if topK <= 0 {
		topK = 5
	}
	if len(docs) == 0 || strings.TrimSpace(query) == "" {
		return []map[string]any{}
	}
	qv := embedText(query, 128)
	type hit struct {
		retrievalDoc
		score float64
	}
	hits := make([]hit, 0, len(docs))
	for _, d := range docs {
		dv := embedText(d.Text, 128)
		s := cosineSimilarity(qv, dv)
		hits = append(hits, hit{retrievalDoc: d, score: s})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if topK > len(hits) {
		topK = len(hits)
	}
	out := make([]map[string]any, 0, topK)
	for i := 0; i < topK; i++ {
		out = append(out, map[string]any{
			"id":    hits[i].ID,
			"score": hits[i].score,
			"text":  hits[i].Text,
		})
	}
	return out
}

func embedText(text string, dim int) []float64 {
	if dim <= 0 {
		dim = 128
	}
	vec := make([]float64, dim)
	toks := tokenize(text)
	if len(toks) == 0 {
		return vec
	}
	tf := map[string]int{}
	for _, tok := range toks {
		tf[tok]++
	}
	total := float64(len(toks))
	for tok, n := range tf {
		h := fnv32(tok)
		idx := int(h % uint32(dim))
		weight := float64(n) / total
		vec[idx] += weight
	}
	normalizeL2(vec)
	return vec
}

func tokenize(text string) []string {
	text = strings.ToLower(text)
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func fnv32(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func normalizeL2(v []float64) {
	var sum float64
	for _, x := range v {
		sum += x * x
	}
	if sum == 0 {
		return
	}
	n := math.Sqrt(sum)
	for i := range v {
		v[i] /= n
	}
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}
