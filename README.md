# kkfly

## remote-exec-runner

Run the same command on many remote hosts concurrently over SSH (with optional non-interactive `sudo`), while streaming output and producing a final summary.

### Install (from GitHub Releases)

```bash
curl -fsSL https://raw.githubusercontent.com/kevin197011/kkfly/main/install.sh | bash
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
- **`private_key_path` (required unless `private_key_content` is set)**: Path to the SSH private key file.
- **`private_key_content` (optional)**: Inline private key content (takes precedence over `private_key_path`).
- **`port` (optional, default `22`)**: SSH port.
- **`hosts` (required)**: List of hosts (IP or hostname).
- **`concurrency` (required, `>= 1`)**: Max number of hosts to run in parallel.
- **`command` (required)**: Shell command to run remotely. Runs via `bash -lc`.
- **`sudo` (optional, default `false`)**: If `true`, runs remote command via `sudo -n bash -lc ...` (non-interactive).
- **`connect_timeout_seconds` (optional, default `10`)**: SSH connect timeout.
- **`command_timeout_seconds` (optional, default `900`)**: Per-host command timeout.
- **`max_output_bytes_per_stream` (optional, default `262144`)**: Max bytes captured per stream (`stdout`/`stderr`) per host.
- **`known_hosts_path` (optional)**: Path to a `known_hosts` file to use for host key verification.
- **`strict_host_key_checking` (optional, default `true`)**: If `false`, do not block on unknown host keys (insecure; useful for fresh environments).
- **`disable_stdout_stderr_print` (optional, default `false`)**: If `true`, suppress streaming `OUT/ERR` lines in the terminal (still captured in JSON if enabled).

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

