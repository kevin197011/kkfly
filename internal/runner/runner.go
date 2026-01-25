package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
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

	// Header (audit-friendly, easy to copy/paste)
	{
		strict := true
		if cfg.StrictHostKeyChecking != nil {
			strict = *cfg.StrictHostKeyChecking
		}
		fmt.Fprintf(
			out,
			"KKFLY RUN  %s  hosts=%d  conc=%d  sudo=%t  strict_host_key=%t\n",
			started.Format("2006-01-02 15:04:05"),
			len(cfg.Hosts),
			cfg.Concurrency,
			cfg.Sudo,
			strict,
		)
		cmdLine := strings.Join(strings.Fields(cfg.Command), " ")
		if cmdLine != "" {
			fmt.Fprintf(out, "CMD: %s\n", cmdLine)
		}
		fmt.Fprintln(out, "")
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
	hostStarted := time.Now()
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
		finished := time.Now()
		dur := finished.Sub(hostStarted).Truncate(time.Millisecond)
		events <- Event{At: finished, Host: host, Kind: "finished", Message: fmt.Sprintf("fail exit=-1 dur=%s err=%v", dur, err)}
		return HostResult{
			Host:     host,
			Status:   StatusFailed,
			ExitCode: -1,
			Started:  hostStarted,
			Finished: finished,
			Duration: dur.String(),
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
		dur := hr.Finished.Sub(hr.Started).Truncate(time.Millisecond)
		events <- Event{At: time.Now(), Host: host, Kind: "finished", Message: fmt.Sprintf("fail exit=%d dur=%s err=%v", hr.ExitCode, dur, execErr)}
		return hr
	}

	if res.ExitCode == 0 {
		hr.Status = StatusSucceeded
		dur := hr.Finished.Sub(hr.Started).Truncate(time.Millisecond)
		events <- Event{At: time.Now(), Host: host, Kind: "finished", Message: fmt.Sprintf("ok exit=0 dur=%s", dur)}
		return hr
	}

	hr.Status = StatusFailed
	{
		dur := hr.Finished.Sub(hr.Started).Truncate(time.Millisecond)
		msg := fmt.Sprintf("fail exit=%d dur=%s", res.ExitCode, dur)
		if hr.Error != "" {
			msg += " err=" + hr.Error
		}
		events <- Event{At: time.Now(), Host: host, Kind: "finished", Message: msg}
	}
	return hr
}

func printEvents(out io.Writer, events <-chan Event, suppressOutputLines bool) {
	const tsFmt = "2006-01-02 15:04:05"
	fmt.Fprintf(out, "%-19s  %-16s  %-7s  %s\n", "TIME", "HOST", "STAGE", "MESSAGE")
	for ev := range events {
		ts := ev.At.Format(tsFmt)
		switch ev.Kind {
		case "stdout", "stderr":
			if suppressOutputLines {
				continue
			}
			kind := "OUT"
			if ev.Kind == "stderr" {
				kind = "ERR"
			}
			fmt.Fprintf(out, "%s  %-16s  %-7s  %s\n", ts, ev.Host, kind, ev.Message)
		default:
			kind := ev.Kind
			switch ev.Kind {
			case "queued":
				kind = "QUEUED"
			case "connecting":
				kind = "CONNECT"
			case "running":
				kind = "RUN"
			case "finished":
				kind = "DONE"
			}

			if ev.Message != "" {
				fmt.Fprintf(out, "%s  %-16s  %-7s  %s\n", ts, ev.Host, kind, ev.Message)
				continue
			}
			fmt.Fprintf(out, "%s  %-16s  %-7s\n", ts, ev.Host, kind)
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

	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "SUMMARY  hosts=%d  ok=%d  failed=%d  duration=%s\n", len(r.Results), ok, fail, r.Duration)
	fmt.Fprintln(out, "")

	if fail > 0 {
		counts := map[string]int{}
		for _, res := range r.Results {
			if res.Status == StatusFailed && res.Error != "" {
				counts[res.Error]++
			}
		}
		type kv struct {
			k string
			v int
		}
		var xs []kv
		for k, v := range counts {
			xs = append(xs, kv{k: k, v: v})
		}
		sort.Slice(xs, func(i, j int) bool {
			if xs[i].v == xs[j].v {
				return xs[i].k < xs[j].k
			}
			return xs[i].v > xs[j].v
		})

		fmt.Fprintln(out, "TOP FAILURES")
		for i := 0; i < len(xs) && i < 3; i++ {
			fmt.Fprintf(out, "%dx  %s\n", xs[i].v, xs[i].k)
		}
		fmt.Fprintln(out, "")
	}

	fmt.Fprintln(out, "RESULTS")

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "HOST\tSTATUS\tEXIT\tDURATION\tERROR")
	for _, res := range r.Results {
		status := "OK"
		if res.Status != StatusSucceeded {
			status = "FAIL"
		}
		errStr := ""
		if res.Error != "" {
			errStr = res.Error
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", res.Host, status, res.ExitCode, res.Duration, errStr)
	}
	_ = tw.Flush()
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
