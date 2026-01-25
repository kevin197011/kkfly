## 1. Specification
- [x] 1.1 Define `remote-exec` requirements and scenarios for YAML config, concurrency, sudo, and monitoring output
- [x] 1.2 Validate OpenSpec change structure in strict mode

## 2. Implementation (Go)
- [x] 2.1 Create Go module and CLI entrypoint
- [x] 2.2 Implement YAML config loading + validation (user, key path, port, hosts, concurrency)
- [x] 2.3 Implement SSH executor (key auth, host key verification, command execution)
- [x] 2.4 Implement concurrency worker pool
- [x] 2.5 Implement lifecycle monitoring (per-host events, durations, exit codes, stdout/stderr capture)
- [x] 2.6 Implement audit-friendly console output formatting (fixed columns + summary table)
- [x] 2.7 Implement optional JSON report output

## 3. Documentation
- [x] 3.1 Add example config file
- [x] 3.2 Update `README.md` with usage examples

## 4. Tests
- [x] 4.1 Add unit tests for config parsing and validation
- [ ] 4.2 Add unit test(s) to ensure durations are non-negative and summary output is stable

