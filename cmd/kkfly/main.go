package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
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
			out = io.MultiWriter(os.Stdout, logFile)
		}
	}

	report, err := runner.Run(context.Background(), cfg, runner.Options{
		JSONOutPath: jsonOut,
		Output:      out,
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
