package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/kevin197011/kkfly/internal/config"
	"github.com/kevin197011/kkfly/internal/sshx"
)

type HostStatus string

const (
	StatusQueued    HostStatus = "queued"
	StatusRunning   HostStatus = "running"
	StatusSucceeded HostStatus = "succeeded"
	StatusFailed    HostStatus = "failed"
)

type HostResult struct {
	Host     string     `json:"host"`
	Status   HostStatus `json:"status"`
	ExitCode int        `json:"exit_code"`
	Started  time.Time  `json:"started"`
	Finished time.Time  `json:"finished"`
	Duration string     `json:"duration"`
	Error    string     `json:"error,omitempty"`

	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
}

type Report struct {
	Started  time.Time    `json:"started"`
	Finished time.Time    `json:"finished"`
	Duration string       `json:"duration"`
	Results  []HostResult `json:"results"`
}

type Event struct {
	At      time.Time
	Host    string
	Kind    string
	Message string
}

type Options struct {
	JSONOutPath string
	Output      io.Writer
}

func Run(ctx context.Context, cfg config.Config, opt Options) (Report, error) {
	started := time.Now()

	out := opt.Output
	if out == nil {
		out = os.Stdout
	}

	events := make(chan Event, 4096)
	resultsCh := make(chan HostResult, len(cfg.Hosts))

	var printWg sync.WaitGroup
	printWg.Add(1)
	go func() {
		defer printWg.Done()
		printEvents(out, events, cfg.DisableStdoutStderrPrint)
	}()

	jobs := make(chan string)
	var wg sync.WaitGroup

	workerCount := cfg.Concurrency
	if workerCount > len(cfg.Hosts) {
		workerCount = len(cfg.Hosts)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for host := range jobs {
				resultsCh <- runOne(ctx, cfg, host, events)
			}
		}()
	}

	for _, h := range cfg.Hosts {
		events <- Event{At: time.Now(), Host: h, Kind: "queued"}
		jobs <- h
	}
	close(jobs)

	wg.Wait()
	close(resultsCh)
	close(events)
	printWg.Wait()

	var results []HostResult
	for r := range resultsCh {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Host < results[j].Host })

	finished := time.Now()
	report := Report{
		Started:  started,
		Finished: finished,
		Duration: finished.Sub(started).String(),
		Results:  results,
	}

	printSummary(out, report)

	if opt.JSONOutPath != "" {
		if err := writeJSON(opt.JSONOutPath, report); err != nil {
			return report, err
		}
	}

	// Non-zero exit should be decided by caller; return error only for tool-level failures.
	return report, nil
}

func runOne(ctx context.Context, cfg config.Config, host string, events chan<- Event) HostResult {
	events <- Event{At: time.Now(), Host: host, Kind: "connecting"}

	strict := true
	if cfg.StrictHostKeyChecking != nil {
		strict = *cfg.StrictHostKeyChecking
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, time.Duration(cfg.ConnectTimeoutSeconds)*time.Second)
	defer dialCancel()

	client, err := sshx.Dial(dialCtx, sshx.ExecConfig{
		User:                  cfg.User,
		Host:                  host,
		Port:                  cfg.Port,
		PrivateKeyPath:        cfg.PrivateKeyPath,
		PrivateKeyContent:     cfg.PrivateKeyContent,
		KnownHostsPath:        cfg.KnownHostsPath,
		StrictHostKeyChecking: strict,
		ConnectTimeout:        time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
	})
	if err != nil {
		events <- Event{At: time.Now(), Host: host, Kind: "finished", Message: fmt.Sprintf("connect error: %v", err)}
		return HostResult{
			Host:     host,
			Status:   StatusFailed,
			ExitCode: -1,
			Error:    err.Error(),
		}
	}
	defer client.Close()

	events <- Event{At: time.Now(), Host: host, Kind: "running"}

	cmdArg := cfg.Command
	quoted := sshx.SingleQuoteForBash(cmdArg)
	remoteCmd := "bash -lc " + quoted
	requestPty := false
	if cfg.Sudo {
		remoteCmd = "sudo -n bash -lc " + quoted
		requestPty = true
	}

	runCtx, runCancel := context.WithTimeout(ctx, time.Duration(cfg.CommandTimeoutSeconds)*time.Second)
	defer runCancel()

	res, execErr := sshx.Exec(
		runCtx,
		client,
		remoteCmd,
		requestPty,
		cfg.MaxOutputBytesPerStream,
		func(sl sshx.StreamLine) {
			if cfg.DisableStdoutStderrPrint {
				return
			}
			if sl.IsStderr {
				events <- Event{At: time.Now(), Host: host, Kind: "stderr", Message: sl.Line}
			} else {
				events <- Event{At: time.Now(), Host: host, Kind: "stdout", Message: sl.Line}
			}
		},
	)

	hr := HostResult{
		Host:     host,
		Started:  res.Started,
		Finished: res.Finished,
		Duration: res.Finished.Sub(res.Started).String(),
		ExitCode: res.ExitCode,
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
	}

	if execErr != nil {
		hr.Status = StatusFailed
		hr.Error = execErr.Error()
		events <- Event{At: time.Now(), Host: host, Kind: "finished", Message: fmt.Sprintf("error: %v", execErr)}
		return hr
	}

	if res.ExitCode == 0 {
		hr.Status = StatusSucceeded
		events <- Event{At: time.Now(), Host: host, Kind: "finished", Message: "exit=0"}
		return hr
	}

	hr.Status = StatusFailed
	events <- Event{At: time.Now(), Host: host, Kind: "finished", Message: fmt.Sprintf("exit=%d", res.ExitCode)}
	return hr
}

func printEvents(out io.Writer, events <-chan Event, suppressOutputLines bool) {
	for ev := range events {
		ts := ev.At.Format(time.RFC3339)
		switch ev.Kind {
		case "stdout", "stderr":
			if suppressOutputLines {
				continue
			}
			fmt.Fprintf(out, "%s [%s] %s: %s\n", ts, ev.Host, ev.Kind, ev.Message)
		default:
			if ev.Message != "" {
				fmt.Fprintf(out, "%s [%s] %s: %s\n", ts, ev.Host, ev.Kind, ev.Message)
			} else {
				fmt.Fprintf(out, "%s [%s] %s\n", ts, ev.Host, ev.Kind)
			}
		}
	}
}

func printSummary(out io.Writer, r Report) {
	var ok, fail int
	for _, res := range r.Results {
		if res.Status == StatusSucceeded {
			ok++
		} else {
			fail++
		}
	}

	fmt.Fprintln(out, "---- summary ----")
	fmt.Fprintf(out, "hosts=%d success=%d failed=%d duration=%s\n", len(r.Results), ok, fail, r.Duration)
	for _, res := range r.Results {
		line := fmt.Sprintf("%-24s %-10s exit=%-4d dur=%s", res.Host, res.Status, res.ExitCode, res.Duration)
		if res.Error != "" {
			line += " err=" + res.Error
		}
		fmt.Fprintln(out, line)
	}
}

func writeJSON(path string, report Report) error {
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
