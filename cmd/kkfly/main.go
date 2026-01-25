package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/kevin197011/kkfly/internal/config"
	"github.com/kevin197011/kkfly/internal/runner"
)

func main() {
	var configPath string
	var jsonMode bool
	var jsonOut string
	flag.StringVar(&configPath, "config", "kkfly.yml", "Path to YAML config file (default: kkfly.yml)")
	flag.BoolVar(&jsonMode, "json", false, "Write JSON report to kkfly.json (disables kkfly.log)")
	flag.StringVar(&jsonOut, "json-out", "", "Write JSON report to this path (optional, disables kkfly.log)")
	flag.Parse()

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
