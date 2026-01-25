# Change: Add remote concurrent execution runner

## Why
We need a reliable way to run the same command on many remote hosts concurrently (via SSH) and observe a complete execution lifecycle (queued → running → finished) with clear monitoring and summary results.

## What Changes
- Add a Go CLI tool that reads a YAML config describing SSH auth, host list, and concurrency.
- Execute a configurable remote shell command on each host (defaulting to `curl -fsSL https://raw.githubusercontent.com/kevin197011/krun/main/lib/check-ip-quality.sh | bash`).
- Support optional `sudo` execution in a non-interactive way.
- Provide full lifecycle monitoring and a final aggregated report.

## Impact
- Affected specs: new capability `remote-exec`
- Affected code: new Go module and CLI under repository root
- Security considerations: SSH host key verification and non-interactive `sudo` behavior

