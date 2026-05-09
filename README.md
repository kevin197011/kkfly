# kkfly

## remote-exec-runner

Run the same command on many remote hosts concurrently over SSH (with optional non-interactive `sudo`), stream per-host output, and emit a final summary that is easy to read and to scrape from logs.

### Install (from GitHub Releases)

```bash
curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.sh | bash
```

- **Linux**: the installer writes to `/usr/local/bin` and expects **root** (use `sudo`).
- **macOS**: install can run without root if your environment allows writing to the install prefix (often you still use `sudo` for `/usr/local/bin`).
- Releases: [github.com/kevin197011/kkfly/releases](https://github.com/kevin197011/kkfly/releases)

### Build

```bash
go build -o kkfly ./cmd/kkfly
```

### CLI

| Flag | Meaning |
|------|---------|
| `-config PATH` | YAML config file (default: `kkfly.yml` in the working directory). |
| `-json` | Write a JSON report to `kkfly.json` (implies **no** `kkfly.log`). |
| `-json-out PATH` | Write JSON to this path (`-json` not required). |
| `-version`, `-v` | Print version and exit. |

**Exit status**

- `0` only when **every** host run succeeds.
- Non-zero if any host fails, config is invalid, or writing the JSON report fails.

**First run**

If the config path does not exist, `kkfly` writes a **`kkfly.yml` template**, prints its path, and exits — edit hosts and command, then run again.

### Output, logs, and collection

During a run:

- **Terminal**: timestamped rows (`TIME HOST STAGE MESSAGE`), with **`DONE` lines showing `[completed/total]`** for rough progress while hosts finish out of order.
- **`CMD` preview**: multi-line `command` in YAML is summarized (not flattened blindly into one unreadable token).
- **`SUMMARY`**: includes **`overall=`** (`success`, `partial_failure`, or `failure`) plus counts and wall-clock duration.

**Logs**

Unless you pass `-json` or `-json-out`, a plain-text **`kkfly.log`** is written in the current directory (same content as the terminal stream, without ANSI color). If the log file cannot be opened, the run continues on stdout only (a warning is printed).

**Machine-friendly footer (`KKFLY_COLLECT`)**

After the human-readable tables, the runner prints a plain block between:

- `<<< KKFLY_COLLECT (plain lines for logs/scripts)`  
- `>>> END KKFLY_COLLECT`

It includes:

- One **key/value** summary line (`overall`, `hosts_total`, `hosts_ok`, `hosts_failed`, `duration`, `wall_seconds`).
- Optional **`failed_hosts_tsv=`** (tab-separated hostnames).
- A **TSV** table between `RESULT_TSV_BEGIN` / `RESULT_TSV_END` with columns: `HOST`, `STATUS`, `EXIT`, `DURATION_MS`, `ERROR`.

Use this block for log pipelines, alerts, or dashboards without parsing ANSI.

**Color**

Set `NO_COLOR` or `KKFLY_NO_COLOR` to force plain output on a tty.

### JSON report

With `-json` or `-json-out`, the report file includes:

- Top-level **`overall`**, **`hosts_total`**, **`hosts_ok`**, **`hosts_failed`**, **`failed_hosts`** (in addition to **`results`**).
- Per host: **`host`**, **`status`**, **`exit_code`**, **`started`**, **`finished`**, **`duration`**, **`error`**, and captured **`stdout`** / **`stderr`** when present.

Example:

```bash
./kkfly --json
jq -e '.overall == "success"' kkfly.json >/dev/null
```

### Configure

Copy and edit:

- `kkfly.yml` (default config file), or
- `configs/kkfly.yml` as a template.

**Required fields**

- `user`
- `hosts` (non-empty)
- `concurrency` (`>= 1`)
- `command`
- One of: `private_key_path` or `private_key_content`

**Optional (common)**

- `port` (default **`22`** if omitted or `0`)
- `sudo`, timeouts, output caps, host-key policy — see table below.

#### `kkfly.yml` configuration reference

Example (`kkfly.yml`):

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

Fields:

- **`user` (required)**: SSH username.
- **`private_key_path` (required unless `private_key_content` is set)**: Path to the SSH private key file (relative paths are resolved from the config file directory).
- **`private_key_content` (optional)**: Inline private key content (takes precedence over `private_key_path`).
- **`port` (optional, default `22`)**: SSH port.
- **`hosts` (required)**: List of hosts (IP or hostname).
- **`concurrency` (required, `>= 1`)**: Max number of hosts to run in parallel (capped at host count internally).
- **`command` (required)**: Shell command to run remotely. Runs via **`bash -lc`** on the remote side.
- **`sudo` (optional, default `false`)**: If `true`, runs the remote command via **`sudo -n bash -lc ...`** (non-interactive). Often combined with requesting a PTY for `sudo`; see sudo section below.
- **`connect_timeout_seconds` (optional, default `10`)**: SSH connect timeout.
- **`command_timeout_seconds` (optional, default `900`)**: Per-host command deadline (applied as a client-side timeout context). Unused / non-positive values default to **`900`** (15 minutes).
- **`max_output_bytes_per_stream` (optional, default `262144`)**: Max bytes captured per stream (**`stdout`** / **`stderr`**) per host; extra output is truncated.
- **`known_hosts_path` (optional)**: Path to a `known_hosts` file used when strict host-key checking is enabled.
- **`strict_host_key_checking` (optional, default `true`)**: If **`false`**, host keys are not verified (**insecure**). If **`true`** and no known-hosts source is available, dial may fail fast with a clear error.
- **`disable_stdout_stderr_print` (optional, default `false`)**: If **`true`**, suppress streaming **`OUT`** / **`ERR`** lines on the tty (captures still apply where implemented).

### Run

```bash
./kkfly
```

Custom config path:

```bash
./kkfly -config /path/to/config.yml
```

Print version:

```bash
./kkfly --version
```

Write a JSON report (default **`kkfly.json`**):

```bash
./kkfly --json
```

Custom JSON output path:

```bash
./kkfly -json-out /tmp/report.json
```

### sudo (non-interactive)

If `sudo: true`, the runner uses **`sudo -n`** (no password prompts). The remote user must have **passwordless sudo** configured for the command pattern you need; otherwise expect a fast failure on that host.
