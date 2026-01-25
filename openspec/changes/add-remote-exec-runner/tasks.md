## 1. Specification
- [ ] 1.1 Define `remote-exec` requirements and scenarios for YAML config, concurrency, sudo, and monitoring output
- [ ] 1.2 Validate OpenSpec change structure in strict mode

## 2. Implementation (Go)
- [ ] 2.1 Create Go module and CLI entrypoint
- [ ] 2.2 Implement YAML config loading + validation (user, key path, port, hosts, concurrency)
- [ ] 2.3 Implement SSH executor (key auth, host key verification, command execution)
- [ ] 2.4 Implement concurrency worker pool
- [ ] 2.5 Implement lifecycle monitoring (per-host events, durations, exit codes, stdout/stderr capture)
- [ ] 2.6 Implement summary output (human-readable) and optional JSON report output

## 3. Documentation
- [ ] 3.1 Add example config file
- [ ] 3.2 Update `README.md` with usage examples

## 4. Tests
- [ ] 4.1 Add unit tests for config parsing and validation

