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
		texts, err := parseEmbeddingInputs(t.Input)
		if err != nil {
			return "", err
		}
		if len(texts) == 0 {
			return "", errors.New("embedding requires non-empty text/prompt")
		}
		backend := strings.ToLower(firstNonEmpty(t.Input["embedding_backend"], t.Input["backend"], e.cfg.EmbeddingBackend, "local"))
		model := firstNonEmpty(t.Input["embedding_model"], t.Input["model"], e.cfg.EmbeddingModel, "nomic-embed-text")
		dim := parsePositiveInt(firstNonEmpty(t.Input["embedding_dimension"], t.Input["dimension"]), e.cfg.EmbeddingDimension)
		if dim <= 0 {
			dim = 384
		}
		vectors, modelVersion, err := e.embedTexts(ctx, backend, model, texts, dim)
		if err != nil {
			return "", err
		}
		for i, v := range vectors {
			if len(v) == 0 {
				return "", fmt.Errorf("embedding backend returned empty vector at index %d", i)
			}
			if err := validateVector(v); err != nil {
				return "", fmt.Errorf("invalid vector at index %d: %w", i, err)
			}
		}
		output["backend"] = backend
		output["model"] = model
		output["model_version"] = modelVersion
		output["dimension"] = len(vectors[0])
		output["text_count"] = len(texts)
		if len(vectors) == 1 {
			output["vector"] = vectors[0]
		}
		output["vectors"] = vectors
	case "retrieval":
		query := firstNonEmpty(t.Input["query"], t.Input["text"], t.Input["prompt"])
		docs, err := decodeDocuments(t.Input)
		if err != nil {
			return "", err
		}
		topK := parsePositiveInt(firstNonEmpty(t.Input["top_k"]), 5)
		backend := strings.ToLower(firstNonEmpty(t.Input["retrieval_backend"], t.Input["backend"], e.cfg.RetrievalBackend, "local"))
		filters, err := parseMetadataFilters(t.Input)
		if err != nil {
			return "", err
		}
		hits, err := e.retrieve(ctx, backend, query, docs, filters, topK)
		if err != nil {
			return "", err
		}
		output["backend"] = backend
		output["top_k"] = topK
		output["filters"] = filters
		output["documents"] = hits
		output["query"] = query
	case "aggregation":
		agg, err := e.aggregateTask(ctx, t.JobID, t.TaskID, t.Input)
		if err != nil {
			return "", err
		}
		for k, v := range agg {
			output[k] = v
		}
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
	if p := lookPathWithFallback("hf"); p != "" {
		cmd := exec.CommandContext(ctx, p, "download", model, "--local-dir", targetDir)
		cmd.Env = processEnvWithPathFallback()
		if out, runErr := cmd.CombinedOutput(); runErr != nil {
			return fmt.Errorf("hf download failed: %v (%s)", runErr, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if p := lookPathWithFallback("huggingface-cli"); p != "" {
		cmd := exec.CommandContext(ctx, p, "download", model, "--local-dir", targetDir, "--local-dir-use-symlinks", "False")
		cmd.Env = processEnvWithPathFallback()
		if out, runErr := cmd.CombinedOutput(); runErr != nil {
			return fmt.Errorf("huggingface-cli download failed: %v (%s)", runErr, strings.TrimSpace(string(out)))
		}
		return nil
	}
	repoURL := "https://huggingface.co/" + model
	gitPath := lookPathWithFallback("git")
	if gitPath == "" {
		return errors.New("no huggingface downloader found (hf, huggingface-cli, git) in PATH")
	}
	cmd := exec.CommandContext(ctx, gitPath, "clone", "--depth", "1", repoURL, targetDir)
	cmd.Env = processEnvWithPathFallback()
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("git clone fallback failed: %v (%s)", runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

type aggregationSource struct {
	SourceTaskID string         `json:"source_task_id"`
	ArtifactURI  string         `json:"artifact_uri,omitempty"`
	Payload      map[string]any `json:"payload"`
}

func (e *Executor) aggregateTask(ctx context.Context, jobID, taskID string, in map[string]string) (map[string]any, error) {
	_ = ctx
	mode := strings.ToLower(firstNonEmpty(in["aggregation_mode"], in["mode"], "structured_report"))
	sources, err := e.loadAggregationSources(jobID, taskID, in)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, errors.New("aggregation requires at least one source (dependency artifact or items_json)")
	}
	result, provenance := reduceAggregationSources(sources)
	if err := validateAggregationResult(result, in); err != nil {
		return nil, err
	}
	return map[string]any{
		"mode":         mode,
		"source_count": len(sources),
		"sources":      sources,
		"result":       result,
		"provenance":   provenance,
	}, nil
}

func (e *Executor) loadAggregationSources(jobID, taskID string, in map[string]string) ([]aggregationSource, error) {
	sources := make([]aggregationSource, 0)
	if raw := strings.TrimSpace(in["items_json"]); raw != "" {
		var items []map[string]any
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return nil, fmt.Errorf("invalid items_json: %w", err)
		}
		for i, item := range items {
			sources = append(sources, aggregationSource{
				SourceTaskID: fmt.Sprintf("inline-%d", i+1),
				Payload:      item,
			})
		}
	}

	depURIs := map[string]string{}
	for k, v := range in {
		if !strings.HasPrefix(k, "dep:") || !strings.HasSuffix(k, ":output_uri") {
			continue
		}
		depTaskID := strings.TrimSuffix(strings.TrimPrefix(k, "dep:"), ":output_uri")
		if strings.TrimSpace(depTaskID) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		depURIs[depTaskID] = strings.TrimSpace(v)
	}
	depIDs := make([]string, 0, len(depURIs))
	for depID := range depURIs {
		depIDs = append(depIDs, depID)
	}
	sort.Strings(depIDs)

	strictDeps := parseBool(in["strict_dependencies"], true)
	for _, depID := range depIDs {
		uri := depURIs[depID]
		payload, err := e.loadArtifactPayload(jobID, taskID, uri)
		if err != nil {
			if strictDeps {
				return nil, fmt.Errorf("load dependency %s: %w", depID, err)
			}
			continue
		}
		sources = append(sources, aggregationSource{
			SourceTaskID: depID,
			ArtifactURI:  uri,
			Payload:      payload,
		})
	}
	return sources, nil
}

func (e *Executor) loadArtifactPayload(jobID, taskID, uri string) (map[string]any, error) {
	path, ok := artifactURIToLocalPath(e.cfg.ArtifactRoot, uri)
	if !ok {
		return nil, fmt.Errorf("unsupported dependency artifact URI %q for %s/%s", uri, jobID, taskID)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("invalid artifact json at %s: %w", path, err)
	}
	return out, nil
}

func artifactURIToLocalPath(root, uri string) (string, bool) {
	const prefix = "artifact://"
	if !strings.HasPrefix(uri, prefix) {
		return "", false
	}
	trimmed := strings.TrimPrefix(uri, prefix)
	if strings.HasPrefix(trimmed, "s3/") {
		return "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 {
		return "", false
	}
	jobID := parts[0]
	taskID := parts[1]
	return filepath.Join(root, jobID, taskID, "output.json"), true
}

func reduceAggregationSources(sources []aggregationSource) (map[string]any, map[string][]string) {
	merged := map[string]any{}
	conflicts := map[string][]any{}
	provenance := map[string][]string{}
	highlights := make([]string, 0)

	for _, src := range sources {
		keys := make([]string, 0, len(src.Payload))
		for k := range src.Payload {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if isSystemAggregationKey(k) {
				continue
			}
			v := src.Payload[k]
			if _, ok := merged[k]; !ok {
				merged[k] = v
			} else if !valuesEqualJSON(merged[k], v) {
				conflicts[k] = appendUniqueAny(conflicts[k], merged[k], v)
			}
			provenance[k] = appendUniqueString(provenance[k], src.SourceTaskID)
			if textVal, ok := asNonEmptyString(v); ok {
				highlights = appendUniqueString(highlights, textVal)
			}
		}
	}

	sort.Strings(highlights)
	if len(highlights) > 12 {
		highlights = highlights[:12]
	}
	return map[string]any{
		"merged_fields": merged,
		"conflicts":     conflicts,
		"highlights":    highlights,
	}, provenance
}

func validateAggregationResult(result map[string]any, in map[string]string) error {
	required := parseRequiredFields(in)
	if len(required) == 0 {
		return nil
	}
	merged, _ := result["merged_fields"].(map[string]any)
	missing := make([]string, 0)
	for _, key := range required {
		if _, ok := merged[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("aggregation missing required fields: %s", strings.Join(missing, ","))
	}
	return nil
}

func parseRequiredFields(in map[string]string) []string {
	if raw := strings.TrimSpace(in["required_fields_json"]); raw != "" {
		var fields []string
		if err := json.Unmarshal([]byte(raw), &fields); err == nil {
			return trimNonEmpty(fields)
		}
	}
	raw := strings.TrimSpace(in["required_fields"])
	if raw == "" {
		return nil
	}
	return trimNonEmpty(strings.Split(raw, ","))
}

func trimNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func isSystemAggregationKey(k string) bool {
	return strings.HasPrefix(k, "dep:") || strings.HasPrefix(k, "_")
}

func appendUniqueAny(dst []any, vals ...any) []any {
	for _, v := range vals {
		seen := false
		for i := range dst {
			if valuesEqualJSON(dst[i], v) {
				seen = true
				break
			}
		}
		if !seen {
			dst = append(dst, v)
		}
	}
	return dst
}

func appendUniqueString(dst []string, vals ...string) []string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		exists := false
		for i := range dst {
			if dst[i] == v {
				exists = true
				break
			}
		}
		if !exists {
			dst = append(dst, v)
		}
	}
	return dst
}

func valuesEqualJSON(a, b any) bool {
	aj, aErr := json.Marshal(a)
	bj, bErr := json.Marshal(b)
	if aErr != nil || bErr != nil {
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
	return string(aj) == string(bj)
}

func asNonEmptyString(v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s, true
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

func (e *Executor) embedTexts(ctx context.Context, backend, model string, texts []string, dim int) ([][]float64, string, error) {
	switch backend {
	case "", "local":
		out := make([][]float64, 0, len(texts))
		for _, t := range texts {
			out = append(out, embedText(t, dim))
		}
		return out, "local-hash-v1", nil
	case "ollama":
		return e.embedWithOllama(ctx, model, texts)
	case "vllm":
		return e.embedOpenAICompatible(ctx, strings.TrimRight(strings.TrimSpace(e.cfg.VLLMBaseURL), "/"), "", model, texts)
	case "remote", "remote_api", "remote-api":
		auth := strings.TrimSpace(e.cfg.RemoteAPIKey)
		if auth != "" {
			auth = "Bearer " + auth
		}
		return e.embedOpenAICompatible(ctx, strings.TrimRight(strings.TrimSpace(e.cfg.RemoteAPIBaseURL), "/"), auth, model, texts)
	default:
		return nil, "", fmt.Errorf("unsupported embedding backend %q", backend)
	}
}

func (e *Executor) embedWithOllama(ctx context.Context, model string, texts []string) ([][]float64, string, error) {
	base := strings.TrimRight(strings.TrimSpace(e.cfg.OllamaBaseURL), "/")
	if base == "" {
		return nil, "", errors.New("SPLAI_OLLAMA_BASE_URL is required for embedding backend=ollama")
	}
	// Prefer batch endpoint when available.
	var batchOut struct {
		Embeddings [][]float64 `json:"embeddings"`
		Model      string      `json:"model"`
	}
	if err := e.postJSONWithRetry(ctx, base+"/api/embed", "", map[string]any{
		"model": model,
		"input": texts,
	}, &batchOut); err == nil && len(batchOut.Embeddings) == len(texts) {
		return batchOut.Embeddings, firstNonEmpty(batchOut.Model, model), nil
	}

	out := make([][]float64, 0, len(texts))
	for _, txt := range texts {
		var single struct {
			Embedding []float64 `json:"embedding"`
			Model     string    `json:"model"`
		}
		if err := e.postJSONWithRetry(ctx, base+"/api/embeddings", "", map[string]any{
			"model":  model,
			"prompt": txt,
		}, &single); err != nil {
			return nil, "", err
		}
		if len(single.Embedding) == 0 {
			return nil, "", errors.New("ollama returned empty embedding")
		}
		out = append(out, single.Embedding)
	}
	return out, model, nil
}

func (e *Executor) embedOpenAICompatible(ctx context.Context, baseURL, auth, model string, texts []string) ([][]float64, string, error) {
	if baseURL == "" {
		return nil, "", errors.New("embedding base URL is required")
	}
	var out struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
		Model string `json:"model"`
	}
	if err := e.postJSONWithRetry(ctx, baseURL+"/v1/embeddings", auth, map[string]any{
		"model": model,
		"input": texts,
	}, &out); err != nil {
		return nil, "", err
	}
	if len(out.Data) != len(texts) {
		return nil, "", fmt.Errorf("embeddings count mismatch: got %d want %d", len(out.Data), len(texts))
	}
	vectors := make([][]float64, 0, len(out.Data))
	for i := range out.Data {
		if len(out.Data[i].Embedding) == 0 {
			return nil, "", fmt.Errorf("empty embedding at index %d", i)
		}
		vectors = append(vectors, out.Data[i].Embedding)
	}
	return vectors, firstNonEmpty(out.Model, model), nil
}

func (e *Executor) postJSONWithRetry(ctx context.Context, url, auth string, reqBody any, out any) error {
	attempts := e.cfg.EmbeddingHTTPRetries + 1
	return postJSONWithRetryAttempts(ctx, url, auth, reqBody, out, attempts)
}

func postJSONWithRetryAttempts(ctx context.Context, url, auth string, reqBody any, out any, attempts int) error {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			sleep := time.Duration(i*250) * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(sleep):
			}
		}
		lastErr = postJSON(ctx, url, auth, reqBody, out)
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
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

func parseEmbeddingInputs(in map[string]string) ([]string, error) {
	if raw := strings.TrimSpace(in["texts_json"]); raw != "" {
		var texts []string
		if err := json.Unmarshal([]byte(raw), &texts); err != nil {
			return nil, fmt.Errorf("invalid texts_json: %w", err)
		}
		out := make([]string, 0, len(texts))
		for _, t := range texts {
			t = strings.TrimSpace(t)
			if t != "" {
				out = append(out, t)
			}
		}
		return out, nil
	}
	text := firstNonEmpty(in["text"], in["prompt"], in["op"])
	if text == "" {
		return nil, nil
	}
	return []string{text}, nil
}

func parsePositiveInt(s string, fallback int) int {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func validateVector(v []float64) error {
	for i := range v {
		if math.IsNaN(v[i]) || math.IsInf(v[i], 0) {
			return fmt.Errorf("vector contains invalid float at index %d", i)
		}
	}
	return nil
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
	ID       string         `json:"id"`
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata,omitempty"`
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
			if d.Metadata == nil {
				d.Metadata = map[string]any{}
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
			out = append(out, retrievalDoc{ID: fmt.Sprintf("doc-%d", i+1), Text: line, Metadata: map[string]any{}})
		}
		return out, nil
	}
	return nil, nil
}

func (e *Executor) retrieve(ctx context.Context, backend, query string, docs []retrievalDoc, filters map[string]string, topK int) ([]map[string]any, error) {
	switch backend {
	case "", "local":
		return retrieveDocumentsLocalHybrid(query, docs, filters, topK), nil
	case "remote", "remote_api", "remote-api":
		return e.retrieveRemote(ctx, query, docs, filters, topK)
	default:
		return nil, fmt.Errorf("unsupported retrieval backend %q", backend)
	}
}

func (e *Executor) retrieveRemote(ctx context.Context, query string, docs []retrievalDoc, filters map[string]string, topK int) ([]map[string]any, error) {
	base := strings.TrimRight(strings.TrimSpace(e.cfg.RetrievalBaseURL), "/")
	if base == "" {
		return nil, errors.New("SPLAI_RETRIEVAL_BASE_URL is required for retrieval backend=remote_api")
	}
	auth := strings.TrimSpace(e.cfg.RetrievalAPIKey)
	if auth != "" {
		auth = "Bearer " + auth
	}
	var out struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := postJSONWithRetryAttempts(ctx, base+"/v1/retrieve", auth, map[string]any{
		"query":     query,
		"documents": docs,
		"filters":   filters,
		"top_k":     topK,
	}, &out, e.cfg.RetrievalHTTPRetries+1); err != nil {
		return nil, err
	}
	return out.Documents, nil
}

func retrieveDocumentsLocalHybrid(query string, docs []retrievalDoc, filters map[string]string, topK int) []map[string]any {
	if topK <= 0 {
		topK = 5
	}
	query = strings.TrimSpace(query)
	if len(docs) == 0 || query == "" {
		return []map[string]any{}
	}
	filtered := filterDocsByMetadata(docs, filters)
	if len(filtered) == 0 {
		return []map[string]any{}
	}
	qv := embedText(query, 128)
	qTokens := tokenize(query)
	type hit struct {
		retrievalDoc
		vectorScore  float64
		lexicalScore float64
		score        float64
	}
	hits := make([]hit, 0, len(filtered))
	for _, d := range filtered {
		dv := embedText(d.Text, 128)
		vectorScore := cosineSimilarity(qv, dv)
		lexicalScore := tokenOverlapScore(qTokens, tokenize(d.Text))
		hybrid := 0.7*vectorScore + 0.3*lexicalScore
		hits = append(hits, hit{
			retrievalDoc: d,
			vectorScore:  vectorScore,
			lexicalScore: lexicalScore,
			score:        hybrid,
		})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if topK > len(hits) {
		topK = len(hits)
	}
	out := make([]map[string]any, 0, topK)
	for i := 0; i < topK; i++ {
		out = append(out, map[string]any{
			"id":            hits[i].ID,
			"score":         hits[i].score,
			"vector_score":  hits[i].vectorScore,
			"lexical_score": hits[i].lexicalScore,
			"text":          hits[i].Text,
			"metadata":      hits[i].Metadata,
		})
	}
	return out
}

func parseMetadataFilters(in map[string]string) (map[string]string, error) {
	if raw := strings.TrimSpace(in["filters_json"]); raw != "" {
		var filters map[string]string
		if err := json.Unmarshal([]byte(raw), &filters); err != nil {
			return nil, fmt.Errorf("invalid filters_json: %w", err)
		}
		return filters, nil
	}
	out := map[string]string{}
	if v := strings.TrimSpace(in["tenant"]); v != "" {
		out["tenant"] = v
	}
	if v := strings.TrimSpace(in["data_classification"]); v != "" {
		out["data_classification"] = v
	}
	return out, nil
}

func filterDocsByMetadata(docs []retrievalDoc, filters map[string]string) []retrievalDoc {
	if len(filters) == 0 {
		return docs
	}
	out := make([]retrievalDoc, 0, len(docs))
	for _, d := range docs {
		if matchesFilters(d.Metadata, filters) {
			out = append(out, d)
		}
	}
	return out
}

func matchesFilters(metadata map[string]any, filters map[string]string) bool {
	for k, want := range filters {
		got, ok := metadata[k]
		if !ok {
			return false
		}
		if !strings.EqualFold(strings.TrimSpace(fmt.Sprintf("%v", got)), strings.TrimSpace(want)) {
			return false
		}
	}
	return true
}

func tokenOverlapScore(q, d []string) float64 {
	if len(q) == 0 || len(d) == 0 {
		return 0
	}
	qs := map[string]struct{}{}
	for _, tok := range q {
		qs[tok] = struct{}{}
	}
	ds := map[string]struct{}{}
	for _, tok := range d {
		ds[tok] = struct{}{}
	}
	intersect := 0
	for tok := range qs {
		if _, ok := ds[tok]; ok {
			intersect++
		}
	}
	denom := len(qs) + len(ds) - intersect
	if denom <= 0 {
		return 0
	}
	return float64(intersect) / float64(denom)
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

func lookPathWithFallback(bin string) string {
	if p, err := exec.LookPath(bin); err == nil {
		return p
	}
	for _, dir := range strings.Split(defaultExecPath(), ":") {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		p := filepath.Join(dir, bin)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func processEnvWithPathFallback() []string {
	env := os.Environ()
	hasPath := false
	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			hasPath = true
			if strings.TrimSpace(strings.TrimPrefix(e, "PATH=")) == "" {
				env[i] = "PATH=" + defaultExecPath()
			}
			break
		}
	}
	if !hasPath {
		env = append(env, "PATH="+defaultExecPath())
	}
	return env
}

func defaultExecPath() string {
	return "/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin"
}
