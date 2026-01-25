# kkfly

## remote-exec-runner

Run the same command on many remote hosts concurrently over SSH (with optional non-interactive `sudo`), while streaming output and producing a final summary.

### Install (from GitHub Releases)

```bash
bash install.sh
```

### Build

```bash
go build -o kkfly ./cmd/kkfly
```

### Configure

Copy and edit:
- `kkfly.yml` (default config file)
- or use `configs/kkfly.yml` as a template

Minimum required fields:
- `user`
- `private_key_path`
- `port`
- `hosts`
- `concurrency`
- `command`

### Run

```bash
./kkfly
```

Print version:

```bash
./kkfly --version
```

Write a JSON report:

```bash
./kkfly --json
```

### sudo (non-interactive)

If `sudo: true`, the runner uses `sudo -n` (no prompts). The remote user must have NOPASSWD sudo permissions, otherwise the host run will fail fast without hanging.

