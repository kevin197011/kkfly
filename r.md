## Remote concurrent runner (Go)

This repository now includes a Go CLI that can execute the following (or any overridden) command on many remote hosts concurrently over SSH:

`curl -fsSL https://raw.githubusercontent.com/kevin197011/krun/main/lib/check-ip-quality.sh | bash`

### Features

- **Batch remote execution**: run the same command across a host list.
- **Concurrency control**: bound parallelism with `concurrency`.
- **sudo support**: optional `sudo -n` (non-interactive).
- **Full lifecycle monitoring**: per-host `queued/connecting/running/finished`, duration, exit code, stdout/stderr streaming.
- **Report**: human summary + optional JSON output.

### YAML config (required fields)

- `user`: SSH username
- `private_key_path`: SSH private key path
- `port`: SSH port
- `hosts`: list of hosts (IP/hostname)
- `concurrency`: max parallel hosts
- `command`: remote command to run

Example: `configs/kkfly.yml`

### Build & run

```bash
go build -o kkfly ./cmd/kkfly
./kkfly
```

JSON report:

```bash
./kkfly -json-out report.json
```

### sudo notes

When `sudo: true`, the runner uses `sudo -n` and will **not** prompt for a password. The remote user must have NOPASSWD permissions, otherwise that host run fails fast.