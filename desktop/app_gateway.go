package main

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ylcn91/pilot/internal/config"
)

func startGatewayProcess(configPath, projectPath string) (*exec.Cmd, error) {
	args := []string{"start", "--config", configPath}
	if projectPath != "" {
		args = append(args, "--project", projectPath)
	}
	cmd := exec.Command("pilot", args...)
	cmd.Env = managedGatewayEnv(os.Environ(), filepath.Dir(configPath))
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func (a *App) EnsureGatewayRunning() ServerStatus {
	status := a.GetServerStatus()
	if status.Running {
		return status
	}

	a.mu.Lock()
	if a.gatewayStarting {
		a.mu.Unlock()
		return a.waitForGateway(10 * time.Second)
	}
	if a.gatewayCmd != nil && a.gatewayStartedByApp {
		a.mu.Unlock()
		return a.waitForGateway(10 * time.Second)
	}
	a.gatewayStarting = true
	a.mu.Unlock()

	projectPath, err := a.getwd()
	if err != nil {
		projectPath = "."
	}
	configPath, runtimeProjectPath, configDir, err := a.prepareManagedGatewayConfig(projectPath)
	if err != nil {
		a.mu.Lock()
		a.gatewayStarting = false
		a.mu.Unlock()
		return ServerStatus{GatewayURL: a.gatewayURL, Error: err.Error()}
	}
	cmd, err := a.startGatewayProcess(configPath, runtimeProjectPath)

	a.mu.Lock()
	a.gatewayStarting = false
	if err != nil {
		a.mu.Unlock()
		_ = os.RemoveAll(configDir)
		return ServerStatus{GatewayURL: a.gatewayURL, Error: err.Error()}
	}
	done := make(chan error, 1)
	a.gatewayCmd = cmd
	a.gatewayDone = done
	a.gatewayStartedByApp = true
	a.gatewayConfigDir = configDir
	a.gatewayProjectPath = runtimeProjectPath
	a.mu.Unlock()

	go a.waitManagedGateway(cmd, done)

	status = a.waitForGateway(10 * time.Second)
	if !status.Running && status.Error == "" {
		status.Error = "gateway did not become healthy"
	}
	return status
}

func (a *App) waitForGateway(timeout time.Duration) ServerStatus {
	deadline := time.Now().Add(timeout)
	for {
		status := a.GetServerStatus()
		if status.Running {
			a.mu.Lock()
			status.StartedByApp = a.gatewayStartedByApp
			a.mu.Unlock()
			return status
		}
		if time.Now().After(deadline) {
			status.Error = "gateway is not reachable"
			return status
		}
		a.sleep(250 * time.Millisecond)
	}
}

func (a *App) waitManagedGateway(cmd *exec.Cmd, done chan error) {
	err := cmd.Wait()
	done <- err
	close(done)

	a.mu.Lock()
	if a.gatewayCmd == cmd {
		a.gatewayCmd = nil
		a.gatewayDone = nil
		a.gatewayStartedByApp = false
		configDir := a.gatewayConfigDir
		a.gatewayConfigDir = ""
		if configDir != "" {
			_ = os.RemoveAll(configDir)
		}
	}
	a.mu.Unlock()
}

func (a *App) stopManagedGateway() {
	a.mu.Lock()
	cmd := a.gatewayCmd
	done := a.gatewayDone
	startedByApp := a.gatewayStartedByApp
	configDir := a.gatewayConfigDir
	a.gatewayCmd = nil
	a.gatewayDone = nil
	a.gatewayStartedByApp = false
	a.gatewayConfigDir = ""
	a.mu.Unlock()

	if !startedByApp || cmd == nil || cmd.Process == nil {
		return
	}

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
	}

	if done == nil {
		return
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
	if configDir != "" {
		_ = os.RemoveAll(configDir)
	}
}

func (a *App) prepareManagedGatewayConfig(projectPath string) (configPath, runtimeProjectPath, configDir string, err error) {
	host, port := parseGatewayAddress(a.gatewayURL)
	configDir, err = os.MkdirTemp("", "pilot-desktop-runtime.")
	if err != nil {
		return "", "", "", err
	}
	memoryPath := filepath.Join(configDir, "memory")
	runtimeProjectPath, err = filepath.Abs(projectPath)
	if err != nil {
		_ = os.RemoveAll(configDir)
		return "", "", "", err
	}
	if err := os.MkdirAll(runtimeProjectPath, 0o755); err != nil {
		_ = os.RemoveAll(configDir)
		return "", "", "", err
	}
	if err := os.MkdirAll(memoryPath, 0o755); err != nil {
		_ = os.RemoveAll(configDir)
		return "", "", "", err
	}
	if err := writeGHBlockShim(configDir); err != nil {
		_ = os.RemoveAll(configDir)
		return "", "", "", err
	}

	configPath = filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(managedGatewayConfigYAML(host, port, runtimeProjectPath, memoryPath)), 0o600); err != nil {
		_ = os.RemoveAll(configDir)
		return "", "", "", err
	}
	return configPath, runtimeProjectPath, configDir, nil
}

func parseGatewayAddress(gatewayURL string) (string, int) {
	if gatewayURL == "" {
		return "127.0.0.1", 9090
	}
	parsed, err := url.Parse(gatewayURL)
	if err != nil {
		return "127.0.0.1", 9090
	}
	host := parsed.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	port := 9090
	if parsed.Port() != "" {
		if parsedPort, err := strconv.Atoi(parsed.Port()); err == nil && parsedPort > 0 {
			port = parsedPort
		}
	}
	return host, port
}

func resolveConfiguredProjectPath(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if cfg.DefaultProject != "" {
		for _, project := range cfg.Projects {
			if project != nil && project.Name == cfg.DefaultProject && project.Path != "" {
				return project.Path
			}
		}
	}
	for _, project := range cfg.Projects {
		if project != nil && project.Path != "" {
			return project.Path
		}
	}
	return ""
}

func managedGatewayConfigYAML(host string, port int, projectPath, memoryPath string) string {
	return fmt.Sprintf(`version: "1.0"
gateway:
  host: %q
  port: %d
  codex_runtime:
    command: "codex"
    sandbox: "read-only"
auth:
  type: "claude-code"
adapters:
  github:
    enabled: false
    token: ""
    repo: ""
    pilot_label: "pilot-disabled"
    polling:
      enabled: false
      interval: 1h
      label: "pilot-disabled"
    stale_label_cleanup:
      enabled: false
    project_board:
      enabled: false
      source_enabled: false
  linear:
    enabled: false
    polling:
      enabled: false
  slack:
    enabled: false
    socket_mode: false
  telegram:
    enabled: false
    polling: false
  gitlab:
    enabled: false
    polling:
      enabled: false
  azure_devops:
    enabled: false
    polling:
      enabled: false
  jira:
    enabled: false
    polling:
      enabled: false
  asana:
    enabled: false
    polling:
      enabled: false
  plane:
    enabled: false
    polling:
      enabled: false
  discord:
    enabled: false
orchestrator:
  model: "codex-runtime"
  max_concurrent: 1
  daily_brief:
    enabled: false
  execution:
    mode: "sequential"
    wait_for_merge: false
    poll_interval: 1h
    pr_timeout: 1m
  autopilot:
    enabled: false
    auto_review: false
    auto_merge: false
    auto_create_issues: false
    notify_on_failure: false
    review_feedback:
      enabled: false
memory:
  path: %q
  cross_project: false
  learning:
    enabled: false
    min_confidence: 1
    max_patterns: 0
    include_anti: false
projects:
  - name: "desktop-runtime"
    path: %q
    navigator: false
    default_branch: "main"
default_project: "desktop-runtime"
alerts:
  enabled: false
`, host, port, memoryPath, projectPath)
}

func writeGHBlockShim(configDir string) error {
	binDir := filepath.Join(configDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	shim := "#!/usr/bin/env sh\n" +
		"echo \"blocked gh invocation from managed desktop runtime: gh $*\" >&2\n" +
		"exit 88\n"
	return os.WriteFile(filepath.Join(binDir, "gh"), []byte(shim), 0o755)
}

func managedGatewayEnv(base []string, configDir string) []string {
	blocked := map[string]struct{}{
		"GITHUB_TOKEN":           {},
		"GH_TOKEN":               {},
		"GITHUB_APP_ID":          {},
		"GITHUB_APP_PRIVATE_KEY": {},
		"GITHUB_WEBHOOK_SECRET":  {},
		"GITLAB_TOKEN":           {},
		"AZURE_DEVOPS_PAT":       {},
		"JIRA_API_TOKEN":         {},
		"ASANA_ACCESS_TOKEN":     {},
		"PLANE_API_KEY":          {},
		"DISCORD_BOT_TOKEN":      {},
		"LINEAR_API_KEY":         {},
		"SLACK_BOT_TOKEN":        {},
		"SLACK_APP_TOKEN":        {},
		"TELEGRAM_BOT_TOKEN":     {},
		"GH_CONFIG_DIR":          {},
	}
	env := make([]string, 0, len(base)+2)
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if ok {
			if _, remove := blocked[key]; remove {
				continue
			}
		}
		env = append(env, item)
	}
	env = append(env, "GH_CONFIG_DIR="+filepath.Join(configDir, "gh-config"))
	env = append(env, "PATH="+filepath.Join(configDir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	return env
}
