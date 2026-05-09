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
	"sync/atomic"
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

// hostColumnWidth picks a HOST column width for aligned stream output.
func hostColumnWidth(hosts []string) int {
	const minW = 16
	const maxW = 52
	w := len("HOST")
	for _, h := range hosts {
		if n := len(h); n > w {
			w = n
		}
	}
	switch {
	case w < minW:
		return minW
	case w > maxW:
		return maxW
	default:
		return w
	}
}

// summarizeCommand collapses whitespace for RUN header previews.
func summarizeCommand(cmd string, maxRunes int) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}

	nonEmptyLines := 0
	for _, line := range strings.Split(cmd, "\n") {
		if strings.TrimSpace(line) != "" {
			nonEmptyLines++
		}
	}
	oneLine := strings.Join(strings.Fields(cmd), " ")
	if nonEmptyLines > 1 {
		if maxRunes > 0 && len(oneLine) > maxRunes {
			return fmt.Sprintf("%s (multi-line command; %d lines)", oneLine[:maxRunes]+"...", nonEmptyLines)
		}
		return fmt.Sprintf("%s (multi-line command; %d lines)", oneLine, nonEmptyLines)
	}

	if maxRunes > 0 && len(oneLine) > maxRunes {
		return oneLine[:maxRunes] + "..."
	}
	return oneLine
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

func oneLineTSVCell(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// printCollectBlock emits a deterministic plain-text footer for scraping and dashboards.
func printCollectBlock(out dualOut, r Report) {
	wallSec := r.Finished.Sub(r.Started).Seconds()

	var b strings.Builder
	b.WriteString("\n<<< KKFLY_COLLECT (plain lines for logs/scripts)\n")
	fmt.Fprintf(
		&b,
		"overall=%s hosts_total=%d hosts_ok=%d hosts_failed=%d duration=%s wall_seconds=%.3f\n",
		r.Overall,
		r.HostsTotal,
		r.HostsOK,
		r.HostsFailed,
		r.Duration,
		wallSec,
	)
	if len(r.FailedHosts) > 0 {
		b.WriteString("failed_hosts_tsv=")
		for i, h := range r.FailedHosts {
			if i > 0 {
				b.WriteByte('\t')
			}
			b.WriteString(oneLineTSVCell(h))
		}
		b.WriteByte('\n')
	}
	b.WriteString("--- RESULT_TSV_BEGIN\n")
	b.WriteString("HOST\tSTATUS\tEXIT\tDURATION_MS\tERROR\n")
	for _, res := range r.Results {
		st := "OK"
		if res.Status != StatusSucceeded {
			st = "FAIL"
		}
		ms := res.Finished.Sub(res.Started).Milliseconds()
		fmt.Fprintf(&b, "%s\t%s\t%d\t%d\t%s\n",
			oneLineTSVCell(res.Host),
			st,
			res.ExitCode,
			ms,
			oneLineTSVCell(res.Error),
		)
	}
	b.WriteString("--- RESULT_TSV_END\n")
	b.WriteString(">>> END KKFLY_COLLECT\n")

	out.write(b.String(), "")
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

	// Header (audit-friendly, easy to copy/paste)
	{
		strict := true
		if cfg.StrictHostKeyChecking != nil {
			strict = *cfg.StrictHostKeyChecking
		}
		plainHeader := fmt.Sprintf(
			"KKFLY RUN  %s  hosts=%d  conc=%d  sudo=%t  strict_host_key=%t\n",
			started.Format("2006-01-02 15:04:05"),
			len(cfg.Hosts),
			cfg.Concurrency,
			cfg.Sudo,
			strict,
		)
		coloredHeader := plainHeader
		if ow.color {
			coloredHeader = colorize(ansiBold+ansiCyan, "KKFLY RUN") + plainHeader[len("KKFLY RUN"):]
		}
		ow.write(plainHeader, coloredHeader)
		if cmdPrev := summarizeCommand(cfg.Command, 180); cmdPrev != "" {
			plain := fmt.Sprintf("CMD: %s\n", cmdPrev)
			colored := plain
			if ow.color {
				colored = colorize(ansiDim, "CMD:") + plain[len("CMD:"):]
			}
			ow.write(plain, colored)
		}
		ow.write("\n", "\n")
	}

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

// splitBracketProgressPrefix splits an optional "[i/n] " prefix from DONE stream lines.
func splitBracketProgressPrefix(s string) (prefix string, rest string) {
	if !strings.HasPrefix(s, "[") {
		return "", s
	}
	idx := strings.Index(s, "] ")
	if idx < 0 {
		return "", s
	}
	return s[:idx+2], s[idx+2:]
}

func stageColor(stage, message string) string {
	switch stage {
	case "QUEUED":
		return ansiDim
	case "CONNECT":
		return ansiYellow
	case "RUN":
		return ansiCyan
	case "OUT":
		return ansiGreen
	case "ERR":
		return ansiRed
	case "DONE":
		if strings.HasPrefix(message, "ok ") || strings.HasPrefix(message, "ok") {
			return ansiGreen
		}
		if strings.HasPrefix(message, "fail ") || strings.HasPrefix(message, "fail") {
			return ansiRed
		}
		return ansiYellow
	default:
		return ""
	}
}

func printEvents(out dualOut, events <-chan Event, suppressOutputLines bool, hostW int, totalHosts int, finished *atomic.Uint32) {
	const tsFmt = "2006-01-02 15:04:05"
	tsColW := 19
	labelRow := fmt.Sprintf("%-*s", tsColW, "TIME")

	plainHeader := fmt.Sprintf("%s  %-*s  %-7s  %s\n", labelRow, hostW, "HOST", "STAGE", "MESSAGE")
	coloredHeader := plainHeader
	if out.color && len(plainHeader) >= tsColW {
		coloredHeader = colorize(ansiBold, plainHeader[:tsColW]) + plainHeader[tsColW:]
	}
	out.write(plainHeader, coloredHeader)

	appendDoneProgress := func(msg string, kind string) string {
		if kind != "finished" || totalHosts <= 0 || finished == nil {
			return msg
		}
		n := finished.Add(1)
		return fmt.Sprintf("[%d/%d] %s", n, totalHosts, msg)
	}

	for ev := range events {
		ts := ev.At.Format(tsFmt)
		tsPadded := fmt.Sprintf("%-*s", tsColW, ts)
		host := ev.Host

		switch ev.Kind {
		case "stdout", "stderr":
			if suppressOutputLines {
				continue
			}
			stage := "OUT"
			if ev.Kind == "stderr" {
				stage = "ERR"
			}

			hostPad := padRight(host, hostW)
			msg := ev.Message

			var plain strings.Builder
			fmt.Fprintf(&plain, "%s  %s  %-7s  %s\n", tsPadded, hostPad, stage, msg)

			colored := plain.String()
			if out.color {
				stageC := stageColor(stage, msg)
				var sb strings.Builder
				sb.WriteString(colorize(ansiDim, tsPadded))
				sb.WriteString("  ")
				sb.WriteString(hostPad)
				sb.WriteString("  ")
				sb.WriteString(coloredField(stageC, stage, 7))
				sb.WriteString("  ")
				sb.WriteString(msg)
				sb.WriteString("\n")
				colored = sb.String()
			}
			out.write(plain.String(), colored)

		default:
			stage := ev.Kind
			switch ev.Kind {
			case "queued":
				stage = "QUEUED"
			case "connecting":
				stage = "CONNECT"
			case "running":
				stage = "RUN"
			case "finished":
				stage = "DONE"
			}

			msg := appendDoneProgress(ev.Message, ev.Kind)
			hostPad := padRight(host, hostW)

			if msg != "" {
				var linePlain strings.Builder
				fmt.Fprintf(&linePlain, "%s  %s  %-7s  %s\n", tsPadded, hostPad, stage, msg)

				coloredStr := linePlain.String()
				if out.color {
					stageC := stageColor(stage, ev.Message)

					renderMsg := msg
					if stage == "DONE" {
						pfx, rest := splitBracketProgressPrefix(msg)
						switch {
						case strings.HasPrefix(rest, "ok "):
							renderMsg = pfx + colorize(ansiGreen, "ok") + rest[len("ok "):]
						case strings.HasPrefix(rest, "fail "):
							renderMsg = pfx + colorize(ansiRed, "fail") + rest[len("fail "):]
						}
					}

					var sb strings.Builder
					sb.WriteString(colorize(ansiDim, tsPadded))
					sb.WriteString("  ")
					sb.WriteString(hostPad)
					sb.WriteString("  ")
					sb.WriteString(coloredField(stageC, stage, 7))
					sb.WriteString("  ")
					sb.WriteString(renderMsg)
					sb.WriteString("\n")
					coloredStr = sb.String()
				}
				out.write(linePlain.String(), coloredStr)
				continue
			}

			var linePlain strings.Builder
			fmt.Fprintf(&linePlain, "%s  %s  %-7s\n", tsPadded, hostPad, stage)

			colored := linePlain.String()
			if out.color {
				stageC := stageColor(stage, "")
				var sb strings.Builder
				sb.WriteString(colorize(ansiDim, tsPadded))
				sb.WriteString("  ")
				sb.WriteString(hostPad)
				sb.WriteString("  ")
				sb.WriteString(coloredField(stageC, stage, 7))
				sb.WriteString("\n")
				colored = sb.String()
			}
			out.write(linePlain.String(), colored)
		}
	}
}

func printSummary(out dualOut, r Report) {
	ok := r.HostsOK
	fail := r.HostsFailed

	out.write("\n", "\n")
	plainSum := fmt.Sprintf(
		"SUMMARY  overall=%s  hosts=%d  ok=%d  failed=%d  duration=%s\n",
		r.Overall,
		len(r.Results),
		ok,
		fail,
		r.Duration,
	)
	coloredSum := plainSum
	if out.color {
		overallC := ansiYellow
		switch r.Overall {
		case "success":
			overallC = ansiGreen
		case "failure":
			overallC = ansiRed
		case "partial_failure":
			overallC = ansiYellow
		}

		var failColored string
		switch {
		case fail == 0:
			failColored = colorize(ansiGreen, "0")
		case fail == len(r.Results):
			failColored = colorize(ansiRed, fmt.Sprintf("%d", fail))
		default:
			failColored = colorize(ansiYellow, fmt.Sprintf("%d", fail))
		}

		coloredSum = fmt.Sprintf(
			"SUMMARY  overall=%s  hosts=%d  ok=%s  failed=%s  duration=%s\n",
			colorize(overallC, r.Overall),
			len(r.Results),
			colorize(ansiGreen, fmt.Sprintf("%d", ok)),
			failColored,
			r.Duration,
		)
	}
	out.write(plainSum, coloredSum)
	out.write("\n", "\n")

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

		out.write("TOP FAILURES\n", colorize(ansiBold, "TOP FAILURES")+"\n")
		for i := 0; i < len(xs) && i < 3; i++ {
			plain := fmt.Sprintf("%dx  %s\n", xs[i].v, xs[i].k)
			colored := plain
			if out.color {
				colored = colorize(ansiRed, fmt.Sprintf("%dx", xs[i].v)) + plain[len(fmt.Sprintf("%dx", xs[i].v)):] // keep remainder
			}
			out.write(plain, colored)
		}
		out.write("\n", "\n")
	}

	// Plain (log-friendly): tabwriter
	{
		var b strings.Builder
		b.WriteString("RESULTS\n")
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
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
		out.write(b.String(), "")
	}

	// Colored (terminal-friendly): fixed-width columns (avoid tabwriter width issues with ANSI)
	if out.color {
		hostW := len("HOST")
		durW := len("DURATION")
		for _, res := range r.Results {
			if len(res.Host) > hostW {
				hostW = len(res.Host)
			}
			if len(res.Duration) > durW {
				durW = len(res.Duration)
			}
		}
		if hostW < 16 {
			hostW = 16
		}

		var b strings.Builder
		b.WriteString(colorize(ansiBold, "RESULTS") + "\n")
		b.WriteString(
			padRight("HOST", hostW) + "  " +
				padRight("STATUS", 6) + "  " +
				padRight("EXIT", 4) + "  " +
				padRight("DURATION", durW) + "  " +
				"ERROR\n",
		)
		for _, res := range r.Results {
			status := "OK"
			statusC := ansiGreen
			if res.Status != StatusSucceeded {
				status = "FAIL"
				statusC = ansiRed
			}
			exitStr := fmt.Sprintf("%d", res.ExitCode)
			errStr := res.Error
			b.WriteString(
				padRight(res.Host, hostW) + "  " +
					coloredField(statusC, status, 6) + "  " +
					padRight(exitStr, 4) + "  " +
					padRight(res.Duration, durW) + "  " +
					errStr + "\n",
			)
		}
		out.write("", b.String())
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
