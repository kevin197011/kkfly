package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/kevin197011/kkfly/internal/config"
	"github.com/kevin197011/kkfly/internal/runner"
)

var (
	// Version is the semantic version, injected at build time via -ldflags.
	// Example: go build -ldflags "-X main.Version=v0.1.5"
	Version = "dev"
	// Commit is the git commit sha, injected at build time via -ldflags.
	Commit = "unknown"
	// BuildDate is the build timestamp, injected at build time via -ldflags.
	BuildDate = "unknown"
)

func buildInfoSetting(key string) (string, bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, s := range bi.Settings {
		if s.Key == key {
			return s.Value, true
		}
	}
	return "", false
}

func versionString() string {
	commit := Commit
	buildDate := BuildDate

	// Fall back to Go's embedded VCS info for local builds, so the value is
	// automatically updated after each commit without requiring -ldflags.
	if commit == "unknown" {
		if v, ok := buildInfoSetting("vcs.revision"); ok && v != "" {
			commit = v
		}
	}
	if buildDate == "unknown" {
		if v, ok := buildInfoSetting("vcs.time"); ok && v != "" {
			buildDate = v
		}
	}

	// Shorten commit for readability.
	if len(commit) > 12 && commit != "unknown" {
		commit = commit[:12]
	}

	if commit == "unknown" && buildDate == "unknown" {
		return Version
	}

	parts := []string{Version}
	if commit != "unknown" {
		parts = append(parts, commit)
	}
	if buildDate != "unknown" {
		parts = append(parts, buildDate)
	}
	return strings.Join(parts, " ")
}

const defaultConfigTemplate = `user: ubuntu
private_key_path: ~/.ssh/id_ed25519
# private_key_content: |
#   -----BEGIN RSA PRIVATE KEY-----
#   ...

port: 22
concurrency: 20

# Optional: run via sudo (non-interactive). Requires NOPASSWD on remote host.
sudo: true

# Optional: timeouts and output limits
connect_timeout_seconds: 10
command_timeout_seconds: 900
max_output_bytes_per_stream: 262144

# Optional: SSH host key verification
# known_hosts_path: ~/.ssh/known_hosts
# strict_host_key_checking: true

# Required: remote command to run
command: |
  curl -fsSL https://raw.githubusercontent.com/kevin197011/krun/main/lib/hello-world.sh | bash

hosts:
  - 1.2.3.4
  - example.com
`

func ensureConfigFile(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	// Auto-generate initial config in current working directory.
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	if err := os.WriteFile(path, []byte(defaultConfigTemplate), 0o644); err != nil {
		return false, err
	}
	fmt.Printf("No config file found. Generated template: %s\n", absPath)
	fmt.Println("Please edit the file and run kkfly again.")
	return true, nil
}

func main() {
	var configPath string
	var jsonMode bool
	var jsonOut string
	var showVersion bool
	flag.StringVar(&configPath, "config", "kkfly.yml", "Path to YAML config file (default: kkfly.yml)")
	flag.BoolVar(&jsonMode, "json", false, "Write JSON report to kkfly.json (disables kkfly.log)")
	flag.StringVar(&jsonOut, "json-out", "", "Write JSON report to this path (optional, disables kkfly.log)")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&showVersion, "v", false, "Alias for --version")
	flag.Parse()

	if showVersion {
		fmt.Println(versionString())
		return
	}

	created, err := ensureConfigFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to prepare config file: %v\n", err)
		os.Exit(1)
	}
	if created {
		return
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to load config: %v\n", err)
		os.Exit(1)
	}

	if jsonMode && jsonOut == "" {
		jsonOut = "kkfly.json"
	}

	out := io.Writer(os.Stdout)
	var logFile *os.File
	if jsonOut == "" {
		logFile, err = os.OpenFile("kkfly.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to open kkfly.log: %v\n", err)
		} else {
			defer logFile.Close()
			out = os.Stdout
		}
	}

	report, err := runner.Run(context.Background(), cfg, runner.Options{
		JSONOutPath: jsonOut,
		Output:      out,
		LogOutput:   logFile,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: run failed: %v\n", err)
		os.Exit(1)
	}

	for _, r := range report.Results {
		if r.Status != runner.StatusSucceeded {
			os.Exit(1)
		}
	}
}
