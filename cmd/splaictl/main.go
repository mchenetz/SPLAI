package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "install":
		runInstall(os.Args[2:])
	case "worker":
		runWorker(os.Args[2:])
	case "verify":
		runVerify(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: splaictl <install|worker|verify> [...]")
}

func runInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	release := fs.String("release", "splai", "helm release name")
	namespace := fs.String("namespace", "splai-system", "kubernetes namespace")
	chart := fs.String("chart", "./charts/splai", "path to chart")
	createNamespace := fs.Bool("create-namespace", true, "create namespace if missing")
	values := fs.String("values", "", "optional values file")
	_ = fs.Parse(args)

	cmdArgs := []string{"upgrade", "--install", *release, *chart, "-n", *namespace}
	if *createNamespace {
		cmdArgs = append(cmdArgs, "--create-namespace")
	}
	if strings.TrimSpace(*values) != "" {
		cmdArgs = append(cmdArgs, "-f", *values)
	}
	cmd := exec.Command("helm", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fatalf("helm install failed: %v", err)
	}
}

func runWorker(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: splaictl worker <token|join> [...]")
		os.Exit(1)
	}
	switch args[0] {
	case "token":
		runWorkerToken(args[1:])
	case "join":
		runWorkerJoin(args[1:])
	default:
		fmt.Fprintln(os.Stderr, "usage: splaictl worker <token|join> [...]")
		os.Exit(1)
	}
}

func runWorkerToken(args []string) {
	if len(args) < 1 || args[0] != "create" {
		fmt.Fprintln(os.Stderr, "usage: splaictl worker token create [--length N]")
		os.Exit(1)
	}
	fs := flag.NewFlagSet("worker token create", flag.ExitOnError)
	length := fs.Int("length", 32, "random bytes before base64url encoding")
	_ = fs.Parse(args[1:])
	if *length < 16 {
		fatalf("length must be >= 16")
	}
	b := make([]byte, *length)
	if _, err := rand.Read(b); err != nil {
		fatalf("generate token: %v", err)
	}
	fmt.Println(base64.RawURLEncoding.EncodeToString(b))
}

func runWorkerJoin(args []string) {
	fs := flag.NewFlagSet("worker join", flag.ExitOnError)
	url := fs.String("url", "", "control plane URL, e.g. http://gateway:8080")
	workerID := fs.String("worker-id", hostnameOr("worker-local"), "worker id")
	token := fs.String("token", "", "optional API token")
	workerBin := fs.String("worker-bin", "splai-worker", "worker executable path")
	service := fs.String("service", defaultServiceType(), "service manager: systemd|launchd|none")
	artifactRoot := fs.String("artifact-root", defaultArtifactRoot(), "artifact root")
	maxTasks := fs.String("max-parallel-tasks", "2", "max parallel tasks")
	heartbeat := fs.String("heartbeat-seconds", "5", "heartbeat interval seconds")
	pollMillis := fs.String("poll-millis", "1500", "poll interval milliseconds")
	envPath := fs.String("env-file", defaultEnvPath(), "env file path")
	_ = fs.Parse(args)

	if strings.TrimSpace(*url) == "" {
		fatalf("--url is required")
	}
	if err := os.MkdirAll(filepath.Dir(*envPath), 0o755); err != nil {
		fatalf("create env dir: %v", err)
	}
	var env bytes.Buffer
	env.WriteString("SPLAI_CONTROL_PLANE_URL=" + quoteEnv(*url) + "\n")
	env.WriteString("SPLAI_WORKER_ID=" + quoteEnv(*workerID) + "\n")
	env.WriteString("SPLAI_ARTIFACT_ROOT=" + quoteEnv(*artifactRoot) + "\n")
	env.WriteString("SPLAI_MAX_PARALLEL_TASKS=" + quoteEnv(*maxTasks) + "\n")
	env.WriteString("SPLAI_HEARTBEAT_SECONDS=" + quoteEnv(*heartbeat) + "\n")
	env.WriteString("SPLAI_POLL_MILLIS=" + quoteEnv(*pollMillis) + "\n")
	if strings.TrimSpace(*token) != "" {
		env.WriteString("SPLAI_API_TOKEN=" + quoteEnv(*token) + "\n")
	}
	if err := os.WriteFile(*envPath, env.Bytes(), 0o600); err != nil {
		fatalf("write env file: %v", err)
	}

	switch strings.ToLower(strings.TrimSpace(*service)) {
	case "none":
		fmt.Printf("env file created at %s\n", *envPath)
		fmt.Printf("start manually: env $(cat %s | xargs) %s\n", *envPath, *workerBin)
	case "systemd":
		installSystemdUserService(*envPath, *workerBin)
	case "launchd":
		installLaunchdService(*envPath, *workerBin)
	default:
		fatalf("unsupported service manager %q", *service)
	}
}

func runVerify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	url := fs.String("url", "http://localhost:8080", "control plane URL")
	workerID := fs.String("worker-id", "", "optional worker id to validate assignment polling")
	token := fs.String("token", "", "optional API token")
	_ = fs.Parse(args)

	healthURL := strings.TrimRight(*url, "/") + "/healthz"
	req, err := http.NewRequest(http.MethodGet, healthURL, nil)
	if err != nil {
		fatalf("health check request build failed: %v", err)
	}
	if strings.TrimSpace(*token) != "" {
		req.Header.Set("X-SPLAI-Token", strings.TrimSpace(*token))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatalf("health check failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		fatalf("health check returned %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	fmt.Printf("ok: %s\n", healthURL)

	if strings.TrimSpace(*workerID) != "" {
		assignURL := strings.TrimRight(*url, "/") + "/v1/workers/" + *workerID + "/assignments?max_tasks=1"
		req2, err := http.NewRequest(http.MethodGet, assignURL, nil)
		if err != nil {
			fatalf("worker assignment request build failed: %v", err)
		}
		if strings.TrimSpace(*token) != "" {
			req2.Header.Set("X-SPLAI-Token", strings.TrimSpace(*token))
		}
		resp2, err := http.DefaultClient.Do(req2)
		if err != nil {
			fatalf("worker assignment check failed: %v", err)
		}
		defer resp2.Body.Close()
		if resp2.StatusCode >= 300 {
			b, _ := io.ReadAll(io.LimitReader(resp2.Body, 1024))
			fatalf("worker assignment check returned %s: %s", resp2.Status, strings.TrimSpace(string(b)))
		}
		fmt.Printf("ok: %s\n", assignURL)
	}
}

func installSystemdUserService(envPath, workerBin string) {
	servicePath := filepath.Join(userHomeOr("."), ".config", "systemd", "user", "splai-worker.service")
	if err := os.MkdirAll(filepath.Dir(servicePath), 0o755); err != nil {
		fatalf("create systemd user dir: %v", err)
	}
	unit := "[Unit]\nDescription=SPLAI Worker Agent\nAfter=network-online.target\n\n" +
		"[Service]\nType=simple\nEnvironmentFile=" + envPath + "\nExecStart=" + workerBin + "\nRestart=always\nRestartSec=3\n\n" +
		"[Install]\nWantedBy=default.target\n"
	if err := os.WriteFile(servicePath, []byte(unit), 0o644); err != nil {
		fatalf("write systemd unit: %v", err)
	}
	runBestEffort("systemctl", "--user", "daemon-reload")
	runBestEffort("systemctl", "--user", "enable", "--now", "splai-worker.service")
	fmt.Printf("systemd user service installed: %s\n", servicePath)
}

func installLaunchdService(envPath, workerBin string) {
	plistPath := filepath.Join(userHomeOr("."), "Library", "LaunchAgents", "io.splai.worker.plist")
	wrapperPath := filepath.Join(userHomeOr("."), ".splai", "run-worker.sh")
	if err := os.MkdirAll(filepath.Dir(wrapperPath), 0o755); err != nil {
		fatalf("create worker wrapper dir: %v", err)
	}
	wrapper := "#!/usr/bin/env bash\nset -a\nsource " + shellEscape(envPath) + "\nset +a\nexec " + shellEscape(workerBin) + "\n"
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o755); err != nil {
		fatalf("write worker wrapper: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		fatalf("create launchd dir: %v", err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>io.splai.worker</string>
  <key>ProgramArguments</key><array><string>` + wrapperPath + `</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict></plist>
`
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		fatalf("write launchd plist: %v", err)
	}
	runBestEffort("launchctl", "unload", plistPath)
	runBestEffort("launchctl", "load", plistPath)
	fmt.Printf("launchd service installed: %s\n", plistPath)
	fmt.Printf("worker wrapper installed: %s\n", wrapperPath)
}

func quoteEnv(v string) string {
	if strings.ContainsAny(v, " \t\n\"'") {
		return "\"" + strings.ReplaceAll(v, "\"", "\\\"") + "\""
	}
	return v
}

func hostnameOr(fallback string) string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return fallback
	}
	return h
}

func defaultServiceType() string {
	if runtime.GOOS == "darwin" {
		return "launchd"
	}
	if runtime.GOOS == "linux" {
		return "systemd"
	}
	return "none"
}

func defaultArtifactRoot() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(userHomeOr("."), "Library", "Application Support", "splai", "artifacts")
	}
	if runtime.GOOS == "linux" {
		return filepath.Join(userHomeOr("."), ".local", "share", "splai", "artifacts")
	}
	return filepath.Join(userHomeOr("."), ".splai", "artifacts")
}

func defaultEnvPath() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(userHomeOr("."), ".splai", "worker.env")
	}
	if runtime.GOOS == "linux" {
		return filepath.Join(userHomeOr("."), ".config", "splai", "worker.env")
	}
	return filepath.Join(userHomeOr("."), ".splai", "worker.env")
}

func userHomeOr(fallback string) string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return fallback
	}
	return h
}

func runBestEffort(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
}

func shellEscape(v string) string {
	if v == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'"
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
