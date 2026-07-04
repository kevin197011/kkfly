package runner

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kevin197011/kkfly/internal/config"
)

const ruleWidth = 62

func rule(ch rune) string {
	return strings.Repeat(string(ch), ruleWidth)
}

func overallLabel(overall string) string {
	switch overall {
	case "success":
		return "SUCCESS"
	case "partial_failure":
		return "PARTIAL"
	default:
		return "FAILED"
	}
}

func overallSymbol(overall string) string {
	switch overall {
	case "success":
		return "✓"
	case "partial_failure":
		return "!"
	default:
		return "✗"
	}
}

func hostColumnWidth(hosts []string) int {
	const minW, maxW = 20, 48
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

func summarizeCommand(cmd string, maxRunes int) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}

	lines := 0
	for _, line := range strings.Split(cmd, "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	oneLine := strings.Join(strings.Fields(cmd), " ")
	if lines > 1 {
		suffix := fmt.Sprintf("  (%d lines)", lines)
		if maxRunes > 0 && len(oneLine) > maxRunes {
			return oneLine[:maxRunes] + "..." + suffix
		}
		return oneLine + suffix
	}
	if maxRunes > 0 && len(oneLine) > maxRunes {
		return oneLine[:maxRunes] + "..."
	}
	return oneLine
}

func runHeaderTags(cfg config.Config) []string {
	strict := true
	if cfg.StrictHostKeyChecking != nil {
		strict = *cfg.StrictHostKeyChecking
	}
	tags := []string{
		fmt.Sprintf("%d hosts", len(cfg.Hosts)),
		fmt.Sprintf("×%d", cfg.Concurrency),
	}
	if cfg.Sudo {
		tags = append(tags, "sudo")
	}
	if !strict {
		tags = append(tags, "insecure-ssh")
	} else {
		tags = append(tags, "accept-new")
	}
	return tags
}

func printRunHeader(ow dualOut, cfg config.Config, started time.Time) {
	tags := strings.Join(runHeaderTags(cfg), " · ")
	when := started.Format("2006-01-02 15:04:05")

	plain := rule('─') + "\n" +
		fmt.Sprintf("  kkfly  ·  %s\n", tags) +
		fmt.Sprintf("  %s\n", when)
	colored := colorize(ansiBold+ansiCyan, rule('─')) + "\n" +
		colorize(ansiBold, "  kkfly") + fmt.Sprintf("  ·  %s\n", tags) +
		colorize(ansiDim, fmt.Sprintf("  %s\n", when))

	if cmd := summarizeCommand(cfg.Command, 120); cmd != "" {
		cmdLine := fmt.Sprintf("  $ %s\n", cmd)
		plain += cmdLine
		colored += colorize(ansiDim, "  $ ") + cmd + "\n"
	}

	plain += rule('─') + "\n\n"
	colored += colorize(ansiDim, rule('─')) + "\n\n"
	ow.write(plain, colored)
}

func stagePlain(kind string) string {
	switch kind {
	case "connecting":
		return "conn"
	case "running":
		return "run"
	case "stdout":
		return "out"
	case "stderr":
		return "err"
	case "finished":
		return "done"
	default:
		return kind
	}
}

func stageColor(kind, message string) string {
	switch kind {
	case "connecting":
		return ansiYellow
	case "running":
		return ansiCyan
	case "stdout":
		return ansiGreen
	case "stderr":
		return ansiRed
	case "finished":
		if strings.HasPrefix(message, "exit 0") {
			return ansiGreen
		}
		return ansiRed
	default:
		return ansiDim
	}
}

func formatFinishedMessage(exitCode int, dur time.Duration, errMsg string) string {
	msg := fmt.Sprintf("exit %d · %s", exitCode, dur.Truncate(time.Millisecond))
	if errMsg != "" {
		msg += " · " + errMsg
	}
	return msg
}

func formatDoneProgress(n, total int, msg string) string {
	if total <= 0 {
		return msg
	}
	return fmt.Sprintf("[%d/%d]  %s", n, total, msg)
}

func printStreamHeader(ow dualOut, hostW int) {
	plain := fmt.Sprintf("  %-*s  %-5s  %s\n", hostW, "HOST", "STAGE", "DETAIL") +
		"  " + strings.Repeat("─", hostW+len("STAGE")+len("DETAIL")+4) + "\n"
	colored := plain
	if ow.color {
		colored = colorize(ansiBold, fmt.Sprintf("  %-*s  %-5s  %s\n", hostW, "HOST", "STAGE", "DETAIL")) +
			colorize(ansiDim, "  "+strings.Repeat("─", hostW+len("STAGE")+len("DETAIL")+4)+"\n")
	}
	ow.write(plain, colored)
}

func printEventLine(ow dualOut, host string, hostW int, kind, message string, doneN, doneTotal int) {
	if kind == "queued" {
		return
	}

	stage := stagePlain(kind)
	hostPad := padRight(host, hostW)

	var detail string
	if kind == "finished" {
		detail = formatDoneProgress(doneN, doneTotal, message)
	} else {
		detail = message
	}

	var plain strings.Builder
	if detail != "" {
		fmt.Fprintf(&plain, "  %s  %-5s  %s\n", hostPad, stage, detail)
	} else {
		fmt.Fprintf(&plain, "  %s  %-5s\n", hostPad, stage)
	}

	colored := plain.String()
	if ow.color {
		sc := stageColor(kind, message)
		var sb strings.Builder
		sb.WriteString("  ")
		sb.WriteString(hostPad)
		sb.WriteString("  ")
		sb.WriteString(coloredField(sc, stage, 5))
		sb.WriteString("  ")
		if kind == "finished" {
			pfx, rest := splitDoneProgress(detail)
			sb.WriteString(pfx)
			if strings.HasPrefix(message, "exit 0") {
				sb.WriteString(colorize(ansiGreen, rest))
			} else {
				sb.WriteString(colorize(ansiRed, rest))
			}
		} else if kind == "stderr" {
			sb.WriteString(colorize(ansiRed, detail))
		} else if detail != "" {
			sb.WriteString(detail)
		}
		sb.WriteString("\n")
		colored = sb.String()
	}
	ow.write(plain.String(), colored)
}

func splitDoneProgress(s string) (prefix, rest string) {
	if !strings.HasPrefix(s, "[") {
		return "", s
	}
	idx := strings.Index(s, "]  ")
	if idx < 0 {
		return "", s
	}
	return s[:idx+3], s[idx+3:]
}

func oneLineTSVCell(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func printSummary(ow dualOut, r Report) {
	sym := overallSymbol(r.Overall)
	label := overallLabel(r.Overall)
	headline := fmt.Sprintf("  %s  %s · %d/%d ok · %s",
		sym, label, r.HostsOK, r.HostsTotal, r.Duration)

	plainBanner := "\n" + rule('═') + "\n" + headline + "\n" + rule('═') + "\n\n"
	coloredBanner := plainBanner
	if ow.color {
		c := ansiYellow
		switch r.Overall {
		case "success":
			c = ansiGreen
		case "failure":
			c = ansiRed
		}
		coloredBanner = "\n" + colorize(c, rule('═')) + "\n" +
			colorize(ansiBold+c, headline) + "\n" +
			colorize(c, rule('═')) + "\n\n"
	}
	ow.write(plainBanner, coloredBanner)

	if r.HostsFailed > 0 {
		printTopFailures(ow, r)
	}
	printResultsTable(ow, r)
}

func printTopFailures(ow dualOut, r Report) {
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
	if len(xs) == 0 {
		return
	}
	sort.Slice(xs, func(i, j int) bool {
		if xs[i].v == xs[j].v {
			return xs[i].k < xs[j].k
		}
		return xs[i].v > xs[j].v
	})

	ow.write("  errors\n", colorize(ansiBold, "  errors")+"\n")
	for i := 0; i < len(xs) && i < 3; i++ {
		plain := fmt.Sprintf("    %d×  %s\n", xs[i].v, xs[i].k)
		colored := plain
		if ow.color {
			colored = "    " + colorize(ansiRed, fmt.Sprintf("%d×", xs[i].v)) + "  " + xs[i].k + "\n"
		}
		ow.write(plain, colored)
	}
	ow.write("\n", "\n")
}

func printResultsTable(ow dualOut, r Report) {
	hostW := len("HOST")
	exitW := len("EXIT")
	timeW := len("TIME")
	for _, res := range r.Results {
		if len(res.Host) > hostW {
			hostW = len(res.Host)
		}
		if len(res.Duration) > timeW {
			timeW = len(res.Duration)
		}
	}
	if hostW < 20 {
		hostW = 20
	}
	if exitW < 4 {
		exitW = 4
	}
	if timeW < 6 {
		timeW = 6
	}

	var plain, colored strings.Builder
	header := fmt.Sprintf("  %-*s  %*s  %-*s  %s\n", hostW, "HOST", exitW, "EXIT", timeW, "TIME", "ERROR")
	plain.WriteString(header)
	if ow.color {
		colored.WriteString(colorize(ansiBold, header))
	} else {
		colored.WriteString(header)
	}

	for _, res := range r.Results {
		exitStr := fmt.Sprintf("%d", res.ExitCode)
		errStr := res.Error
		linePlain := fmt.Sprintf("  %-*s  %*s  %-*s  %s\n", hostW, res.Host, exitW, exitStr, timeW, res.Duration, errStr)
		plain.WriteString(linePlain)

		if ow.color {
			statusC := ansiGreen
			if res.Status != StatusSucceeded {
				statusC = ansiRed
			}
			colored.WriteString("  ")
			colored.WriteString(coloredField(statusC, padRight(res.Host, hostW), hostW))
			colored.WriteString("  ")
			colored.WriteString(padRight(exitStr, exitW+2))
			colored.WriteString("  ")
			colored.WriteString(padRight(res.Duration, timeW))
			if errStr != "" {
				colored.WriteString("  ")
				colored.WriteString(colorize(ansiRed, errStr))
			}
			colored.WriteString("\n")
		} else {
			colored.WriteString(linePlain)
		}
	}
	plain.WriteString("\n")
	colored.WriteString("\n")
	ow.write(plain.String(), colored.String())
}

func printCollectBlock(ow dualOut, r Report) {
	wallSec := r.Finished.Sub(r.Started).Seconds()

	var b strings.Builder
	b.WriteString("\n# --- KKFLY_COLLECT ---\n")
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
	b.WriteString("# RESULT_TSV\n")
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
	b.WriteString("# --- END KKFLY_COLLECT ---\n")
	ow.write(b.String(), "")
}
