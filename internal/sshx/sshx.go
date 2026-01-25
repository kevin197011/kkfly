package sshx

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type ExecConfig struct {
	User                  string
	Host                  string
	Port                  int
	PrivateKeyPath        string
	PrivateKeyContent     string
	KnownHostsPath        string
	StrictHostKeyChecking bool

	ConnectTimeout time.Duration
}

type StreamLine struct {
	IsStderr bool
	Line     string
}

type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Started  time.Time
	Finished time.Time
}

type LimitedBuffer struct {
	MaxBytes  int
	Buf       bytes.Buffer
	Truncated bool
}

func (lb *LimitedBuffer) Write(p []byte) (int, error) {
	if lb.MaxBytes <= 0 {
		return lb.Buf.Write(p)
	}
	remain := lb.MaxBytes - lb.Buf.Len()
	if remain <= 0 {
		lb.Truncated = true
		return len(p), nil
	}
	if len(p) <= remain {
		return lb.Buf.Write(p)
	}
	_, _ = lb.Buf.Write(p[:remain])
	lb.Truncated = true
	return len(p), nil
}

func Dial(ctx context.Context, cfg ExecConfig) (*ssh.Client, error) {
	signer, err := readPrivateKey(cfg.PrivateKeyPath, cfg.PrivateKeyContent)
	if err != nil {
		return nil, err
	}

	hostKeyCallback, err := buildHostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}

	clientCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         cfg.ConnectTimeout,
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	var d net.Dialer
	d.Timeout = cfg.ConnectTimeout
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	cc, chans, reqs, err := ssh.NewClientConn(conn, addr, clientCfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return ssh.NewClient(cc, chans, reqs), nil
}

func Exec(
	ctx context.Context,
	client *ssh.Client,
	command string,
	requestPty bool,
	maxOutputBytesPerStream int,
	lineSink func(StreamLine),
) (ExecResult, error) {
	var res ExecResult
	res.Started = time.Now()

	sess, err := client.NewSession()
	if err != nil {
		return ExecResult{}, err
	}
	defer sess.Close()

	if requestPty {
		// Some sudo setups require a TTY. This remains non-interactive.
		_ = sess.RequestPty("xterm", 80, 40, ssh.TerminalModes{
			ssh.ECHO:          0,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		})
	}

	stdoutPipe, err := sess.StdoutPipe()
	if err != nil {
		return ExecResult{}, err
	}
	stderrPipe, err := sess.StderrPipe()
	if err != nil {
		return ExecResult{}, err
	}

	var stdoutBuf LimitedBuffer
	stdoutBuf.MaxBytes = maxOutputBytesPerStream
	var stderrBuf LimitedBuffer
	stderrBuf.MaxBytes = maxOutputBytesPerStream

	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})

	go func() {
		defer close(stdoutDone)
		stream(stdoutPipe, false, &stdoutBuf, lineSink)
	}()
	go func() {
		defer close(stderrDone)
		stream(stderrPipe, true, &stderrBuf, lineSink)
	}()

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- sess.Run(command)
	}()

	var runErr error
	select {
	case <-ctx.Done():
		_ = client.Close()
		runErr = ctx.Err()
	case runErr = <-runErrCh:
	}

	<-stdoutDone
	<-stderrDone

	res.Stdout = stdoutBuf.Buf.String()
	res.Stderr = stderrBuf.Buf.String()
	res.ExitCode = 0
	res.Finished = time.Now()

	if runErr == nil {
		return res, nil
	}

	var exitErr *ssh.ExitError
	if errors.As(runErr, &exitErr) {
		res.ExitCode = exitErr.ExitStatus()
		return res, nil
	}
	res.ExitCode = -1
	return res, runErr
}

func stream(r io.Reader, isStderr bool, buf io.Writer, sink func(StreamLine)) {
	sc := bufio.NewScanner(r)
	// Increase default token size.
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		_, _ = io.WriteString(buf, line+"\n")
		if sink != nil {
			sink(StreamLine{IsStderr: isStderr, Line: line})
		}
	}
	// Ignore scanner errors: SSH stream closes often without extra context.
}

func readPrivateKey(path, content string) (ssh.Signer, error) {
	if strings.TrimSpace(content) != "" {
		return parsePrivateKeyBytes([]byte(content))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parsePrivateKeyBytes(b)
}

func parsePrivateKeyBytes(b []byte) (ssh.Signer, error) {
	s, err := ssh.ParsePrivateKey(b)
	if err == nil {
		return s, nil
	}

	// If the key is encrypted, return a clear message (non-interactive by design).
	if strings.Contains(err.Error(), "encrypted") {
		return nil, fmt.Errorf("private key appears to be encrypted: %w (use an unencrypted key or extend tool to support passphrases)", err)
	}
	return nil, err
}

func buildHostKeyCallback(cfg ExecConfig) (ssh.HostKeyCallback, error) {
	// If strict host key checking is disabled, skip known_hosts verification entirely.
	// This is equivalent to OpenSSH "StrictHostKeyChecking=no".
	if !cfg.StrictHostKeyChecking {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	khPath := strings.TrimSpace(cfg.KnownHostsPath)
	if khPath == "" {
		// Prefer user's default known_hosts if present.
		if u, err := user.Current(); err == nil && u.HomeDir != "" {
			candidate := filepath.Join(u.HomeDir, ".ssh", "known_hosts")
			if _, statErr := os.Stat(candidate); statErr == nil {
				khPath = candidate
			}
		}
	}

	if khPath != "" {
		cb, err := knownhosts.New(khPath)
		if err != nil {
			return nil, err
		}
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			// knownhosts compares hostnames; we additionally normalize bracketed IPv6 vs plain.
			return cb(hostname, remote, key)
		}, nil
	}

	return nil, errors.New("strict_host_key_checking is enabled but no known_hosts file was found or configured")
}

// SingleQuoteForBash returns a string wrapped for safe usage as a single bash argument.
// Example: abc'def -> 'abc'"'"'def'
func SingleQuoteForBash(s string) string {
	// Fast path: no single quotes.
	if strings.IndexByte(s, '\'') < 0 {
		return "'" + s + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
