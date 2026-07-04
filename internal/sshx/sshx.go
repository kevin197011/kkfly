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
	"sync"
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
	// StrictHostKeyChecking=no
	if !cfg.StrictHostKeyChecking {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	khPath := strings.TrimSpace(cfg.KnownHostsPath)
	if khPath == "" {
		var err error
		khPath, err = defaultKnownHostsPath()
		if err != nil {
			return nil, err
		}
	}

	// OpenSSH accept-new: verify known keys; auto-create file; append on first connect.
	return hostKeyCallbackAcceptNew(khPath)
}

func defaultKnownHostsPath() (string, error) {
	u, err := user.Current()
	if err != nil || u.HomeDir == "" {
		return "", errors.New("cannot resolve home directory for known_hosts")
	}
	return filepath.Join(u.HomeDir, ".ssh", "known_hosts"), nil
}

func ensureKnownHostsFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func hostForKnownHosts(hostname string, remote net.Addr) string {
	if _, _, err := net.SplitHostPort(hostname); err == nil {
		return hostname
	}
	if remote != nil {
		return remote.String()
	}
	return hostname
}

var knownHostsMu sync.Mutex

func appendKnownHost(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	line := knownhosts.Line([]string{hostForKnownHosts(hostname, remote)}, key)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
}

func hostKeyCallbackAcceptNew(path string) (ssh.HostKeyCallback, error) {
	if err := ensureKnownHostsFile(path); err != nil {
		return nil, fmt.Errorf("prepare known_hosts %s: %w", path, err)
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		knownHostsMu.Lock()
		defer knownHostsMu.Unlock()

		verify, err := knownhosts.New(path)
		if err != nil {
			return err
		}
		if err := verify(hostname, remote, key); err == nil {
			return nil
		} else {
			var keyErr *knownhosts.KeyError
			if !errors.As(err, &keyErr) || len(keyErr.Want) > 0 {
				return err
			}
			if err := appendKnownHost(path, hostname, remote, key); err != nil {
				return err
			}
			verify, err = knownhosts.New(path)
			if err != nil {
				return err
			}
			return verify(hostname, remote, key)
		}
	}, nil
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
