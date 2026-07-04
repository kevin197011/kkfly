# kkfly

Concurrent SSH remote command runner — run the same shell command on many hosts in parallel, stream per-host output, and get a human-readable summary plus machine-scrapeable results.

## Quick start

```bash
# 1. Install
curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.sh | bash

# 2. First run generates kkfly.yml — edit hosts + command, then:
kkfly

# 3. Optional: JSON report for automation
kkfly --json
jq -e '.overall == "success"' kkfly.json
```

## Install

**Latest release** (Linux / macOS, `amd64` / `arm64`):

```bash
curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.sh | bash
```

**Pin version or install path:**

```bash
VERSION=0.1.13 curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.sh | bash
curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.sh | bash -s -- -v 0.1.13
INSTALL_DIR=~/.local/bin curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.sh | bash
```

| Platform | Install path | Notes |
|----------|--------------|-------|
| Linux | `/usr/local/bin` | Use `sudo` when the directory is not writable |
| macOS | `/usr/local/bin` or `~/.local/bin` | Falls back to `~/.local/bin` without root |

Artifacts are verified against the release **`checksums.txt`** (SHA-256).  
Releases: [github.com/kevin197011/kkfly/releases](https://github.com/kevin197011/kkfly/releases)

## Usage

```bash
kkfly                              # default config: ./kkfly.yml
kkfly -config /path/to/config.yml
kkfly --version
kkfly --json                       # write kkfly.json (no kkfly.log)
kkfly -json-out /tmp/report.json
```

| Flag | Description |
|------|-------------|
| `-config PATH` | YAML config (default: `kkfly.yml` in cwd) |
| `-json` | Write JSON report to `kkfly.json` |
| `-json-out PATH` | Write JSON to this path |
| `-version`, `-v` | Print version and exit |

**Exit code:** `0` only when every host succeeds; non-zero on any host failure, bad config, or JSON write error.

**First run:** if the config file is missing, `kkfly` writes a `kkfly.yml` template, prints its path, and exits.

## Configuration

Copy [`configs/kkfly.yml`](configs/kkfly.yml) or edit the auto-generated `kkfly.yml`:

```yaml
user: ubuntu
private_key_path: ~/.ssh/id_ed25519
port: 22
concurrency: 20
sudo: true
connect_timeout_seconds: 10
command_timeout_seconds: 900
max_output_bytes_per_stream: 262144
strict_host_key_checking: false
command: |
  echo hello
hosts:
  - 1.2.3.4
  - example.com
```

### Fields

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `user` | yes | — | SSH username |
| `private_key_path` | yes* | — | Path to SSH private key (`~` expanded; relative to config dir) |
| `private_key_content` | yes* | — | Inline key; takes precedence over `private_key_path` |
| `hosts` | yes | — | Hostnames or IPs (non-empty list) |
| `concurrency` | yes | — | Max parallel hosts (≥ 1, capped at host count) |
| `command` | yes | — | Remote shell command (`bash -lc` on remote) |
| `port` | no | `22` | SSH port |
| `sudo` | no | `false` | Run via `sudo -n bash -lc` (needs NOPASSWD on remote) |
| `connect_timeout_seconds` | no | `10` | SSH dial timeout |
| `command_timeout_seconds` | no | `900` | Per-host command deadline (15 min) |
| `max_output_bytes_per_stream` | no | `262144` | Max captured bytes per stdout/stderr stream |
| `known_hosts_path` | no | `~/.ssh/known_hosts` | Used when strict checking is on |
| `strict_host_key_checking` | no | `true` | `false` skips host-key verification (insecure) |
| `disable_stdout_stderr_print` | no | `false` | Suppress live `out`/`err` lines (capture still applies) |

\* One of `private_key_path` or `private_key_content` is required. Encrypted keys are not supported (non-interactive by design).

## Output

Unless `-json` / `-json-out` is set, a plain-text **`kkfly.log`** is written alongside terminal output (no ANSI). Log open failures are warned and ignored.

Set `NO_COLOR` or `KKFLY_NO_COLOR` to disable terminal colors.

### Run header

```
──────────────────────────────────────────────────────────────
  kkfly  ·  2 hosts · ×20 · sudo
  2026-07-05 01:30:05
  $ curl -fsSL ... | bash  (3 lines)
──────────────────────────────────────────────────────────────
```

### Live stream

No per-line timestamps (time is in the header). Queued hosts are omitted.

```
  HOST                  STAGE  DETAIL
  ─────────────────────────────────────────────────────────
  1.2.3.4               conn
  1.2.3.4               run
  1.2.3.4               out    hello
  1.2.3.4               done   [2/5]  exit 0 · 1.2s
```

Stages: `conn` → `run` → `out` / `err` → `done`.

### Summary

```
══════════════════════════════════════════════════════════════
  ✓  SUCCESS · 2/2 ok · 5.123s
══════════════════════════════════════════════════════════════

  errors                         # only when failures exist
    2×  dial tcp: i/o timeout

  HOST                  EXIT  TIME    ERROR
  1.2.3.4                  0  1.2s
  example.com              1  3.4s    connection refused
```

`overall`: `success` · `partial_failure` · `failure`

### KKFLY_COLLECT (log scraping)

Plain footer for pipelines — grep between the markers:

```
# --- KKFLY_COLLECT ---
overall=success hosts_total=2 hosts_ok=2 hosts_failed=0 duration=5.123s wall_seconds=5.123
failed_hosts_tsv=bad.host	other.host
# RESULT_TSV
HOST	STATUS	EXIT	DURATION_MS	ERROR
1.2.3.4	OK	0	1200
bad.host	FAIL	1	3400	connection refused
# --- END KKFLY_COLLECT ---
```

## JSON report

`-json` / `-json-out` writes a structured report (and skips `kkfly.log`):

| Top-level | Per host (`results[]`) |
|-----------|------------------------|
| `overall`, `hosts_total`, `hosts_ok`, `hosts_failed`, `failed_hosts` | `host`, `status`, `exit_code`, `started`, `finished`, `duration`, `error`, `stdout`, `stderr` |

`status` values: `succeeded`, `failed`.

## Build from source

```bash
go build -o kkfly ./cmd/kkfly
```

## Publish (maintainers)

`ruby push.rb` runs the full release pipeline:

```
go test ./...  →  git add/commit/pull/push  →  git tag vX.Y.Z  →  git push tag
                                                      ↓
                              GitHub Actions (GoReleaser) → GitHub Release + binaries
```

```bash
ruby push.rb                    # auto bump patch (v0.1.16 → v0.1.17)
VERSION=0.2.0 ruby push.rb      # pin version
SKIP_RELEASE=1 ruby push.rb     # push code only, no tag
SKIP_TEST=1 ruby push.rb        # skip go test
KK_GIT_DRY_RUN=1 ruby push.rb   # dry-run git steps
```

If `HEAD` already has a `v*.*.*` tag, the release step is skipped (safe to re-run after a no-op push).

Requires the [`kk-git`](https://rubygems.org/gems/kk-git) gem for conventional commit messages and auto push.

## License

MIT — see [LICENSE](LICENSE).
