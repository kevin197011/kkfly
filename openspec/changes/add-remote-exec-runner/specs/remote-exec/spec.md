## ADDED Requirements

### Requirement: YAML configuration
The system SHALL load a YAML configuration file defining SSH access and execution parameters.

#### Scenario: Minimal required fields
- **WHEN** a config file includes `user`, (`private_key_path` OR `private_key_content`), `port`, `hosts`, and `concurrency`
- **THEN** the system runs using those values without prompting for interactive input

#### Scenario: Invalid configuration
- **WHEN** the config file omits required fields or provides invalid values (e.g. concurrency < 1)
- **THEN** the system fails fast with a clear error message and a non-zero exit code

### Requirement: Remote command execution
The system SHALL execute a remote shell command on each configured host over SSH.

#### Scenario: Command provided
- **WHEN** a `command` is provided in the config
- **THEN** the system executes that command

### Requirement: Concurrency control
The system SHALL execute against the host list concurrently, bounded by a configured maximum concurrency.

#### Scenario: Bounded parallelism
- **WHEN** `concurrency` is set to N
- **THEN** the system runs at most N hosts at the same time

### Requirement: Non-interactive sudo support
The system SHALL support executing the remote command with `sudo` in a non-interactive manner.

#### Scenario: Sudo enabled with NOPASSWD
- **WHEN** `sudo: true` is configured and the remote user has NOPASSWD sudo permissions
- **THEN** the command runs successfully under sudo

#### Scenario: Sudo enabled without NOPASSWD
- **WHEN** `sudo: true` is configured and the remote user requires a password for sudo
- **THEN** the system fails the host execution without hanging or prompting for input

### Requirement: Full execution lifecycle monitoring
The system SHALL emit lifecycle events and produce a final report for each host including status, timestamps, duration, and exit information.

#### Scenario: Lifecycle and summary
- **WHEN** a run completes across all hosts
- **THEN** the system prints a summary including counts of success/failure and per-host durations and exit codes

#### Scenario: Audit-friendly event formatting
- **WHEN** the system prints lifecycle events to the terminal
- **THEN** each event SHALL be formatted as fixed columns in a single line using the shape: `YYYY-MM-DD HH:MM:SS  HOST  STAGE  MESSAGE?`
- **AND** `STAGE` SHALL be one of: `QUEUED`, `CONNECT`, `RUN`, `DONE`, `OUT`, `ERR`
- **AND** the output SHALL be readable when copied into tickets or chat (no mandatory ANSI colors)

#### Scenario: Summary table formatting
- **WHEN** the run reaches the final summary
- **THEN** the summary SHALL include an aggregate line with total hosts, succeeded count, failed count, and total duration
- **AND** the summary SHALL include a tabular list with a header row and columns for at least: host, status, exit code, duration, and error
- **AND** the per-host rows SHALL be sorted by host for stable review

