package runner

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	Duration string    `json:"duration"`

	// Aggregates (filled by finalizeReport) for quick scanning and JSON consumers.
	Overall     string   `json:"overall"`
	HostsTotal  int      `json:"hosts_total"`
	HostsOK     int      `json:"hosts_ok"`
	HostsFailed int      `json:"hosts_failed"`
	FailedHosts []string `json:"failed_hosts,omitempty"`

	Results []HostResult `json:"results"`
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
	LogOutput   io.Writer
}

const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[90m"

	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

type dualOut struct {
	term  io.Writer
	log   io.Writer
	color bool
}

func (d dualOut) write(plain, colored string) {
	if d.term != nil {
		if d.color && colored != "" {
			_, _ = io.WriteString(d.term, colored)
		} else {
			_, _ = io.WriteString(d.term, plain)
		}
	}
	if d.log != nil {
		_, _ = io.WriteString(d.log, plain)
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func supportsColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("KKFLY_NO_COLOR") != "" {
		return false
	}
	term := strings.ToLower(os.Getenv("TERM"))
	if term == "" || term == "dumb" {
		return false
	}
	return isTerminal(w)
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func colorize(code, s string) string {
	return code + s + ansiReset
}

func coloredField(code, s string, width int) string {
	if width < 0 {
		width = 0
	}
	pad := width - len(s)
	if pad < 0 {
		pad = 0
	}
	return code + s + ansiReset + strings.Repeat(" ", pad)
}

// finalizeReport fills aggregate counters for summaries, JSON exports, and copy blocks.
func finalizeReport(r *Report) {
	r.HostsTotal = len(r.Results)
	r.HostsOK = 0
	r.HostsFailed = 0
	r.FailedHosts = nil

	for _, x := range r.Results {
		if x.Status == StatusSucceeded {
			r.HostsOK++
		} else {
			r.HostsFailed++
			r.FailedHosts = append(r.FailedHosts, x.Host)
		}
	}

	switch {
	case r.HostsTotal == 0:
		r.Overall = "failure"
	case r.HostsFailed == 0:
		r.Overall = "success"
	case r.HostsOK == 0:
		r.Overall = "failure"
	default:
		r.Overall = "partial_failure"
	}
}

func Run(ctx context.Context, cfg config.Config, opt Options) (Report, error) {
	started := time.Now()

	termOut := opt.Output
	if termOut == nil {
		termOut = os.Stdout
	}
	ow := dualOut{
		term:  termOut,
		log:   opt.LogOutput,
		color: supportsColor(termOut),
	}

	printRunHeader(ow, cfg, started)

	hostColW := hostColumnWidth(cfg.Hosts)
	var finishedCounter atomic.Uint32
	totalHosts := len(cfg.Hosts)

	events := make(chan Event, 4096)
	resultsCh := make(chan HostResult, len(cfg.Hosts))

	var printWg sync.WaitGroup
	printWg.Add(1)
	go func() {
		defer printWg.Done()
		printEvents(ow, events, cfg.DisableStdoutStderrPrint, hostColW, totalHosts, &finishedCounter)
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
		Duration: finished.Sub(started).Round(time.Millisecond).String(),
		Results:  results,
	}
	finalizeReport(&report)

	printSummary(ow, report)
	printCollectBlock(ow, report)

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
		dur := finished.Sub(hostStarted)
		events <- Event{At: finished, Host: host, Kind: "finished", Message: formatFinishedMessage(-1, dur, err.Error())}
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
		events <- Event{At: time.Now(), Host: host, Kind: "finished", Message: formatFinishedMessage(hr.ExitCode, hr.Finished.Sub(hr.Started), execErr.Error())}
		return hr
	}

	if res.ExitCode == 0 {
		hr.Status = StatusSucceeded
		events <- Event{At: time.Now(), Host: host, Kind: "finished", Message: formatFinishedMessage(0, hr.Finished.Sub(hr.Started), "")}
		return hr
	}

	hr.Status = StatusFailed
	events <- Event{At: time.Now(), Host: host, Kind: "finished", Message: formatFinishedMessage(res.ExitCode, hr.Finished.Sub(hr.Started), hr.Error)}
	return hr
}

func printEvents(out dualOut, events <-chan Event, suppressOutputLines bool, hostW int, totalHosts int, finished *atomic.Uint32) {
	printStreamHeader(out, hostW)

	for ev := range events {
		if ev.Kind == "stdout" || ev.Kind == "stderr" {
			if suppressOutputLines {
				continue
			}
		}

		doneN := 0
		if ev.Kind == "finished" && totalHosts > 0 && finished != nil {
			doneN = int(finished.Add(1))
		}
		printEventLine(out, ev.Host, hostW, ev.Kind, ev.Message, doneN, totalHosts)
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
