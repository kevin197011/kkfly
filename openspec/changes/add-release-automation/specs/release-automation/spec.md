## ADDED Requirements

### Requirement: Automated GitHub Release
The system SHALL automatically create a GitHub Release with attached binaries when a version tag is pushed.

#### Scenario: Tag push triggers release
- **WHEN** a tag matching `v*` is pushed to the repository
- **THEN** a GitHub Release is created and includes platform-specific artifacts and checksums

### Requirement: Standardized artifact naming and checksums
The system SHALL publish artifacts with consistent naming and a checksum manifest to enable verification.

#### Scenario: Checksums are published
- **WHEN** a release is published
- **THEN** the release includes a `checksums.txt` file containing SHA-256 checksums for each artifact

### Requirement: Installer downloads correct platform artifact
The system SHALL provide a non-interactive installer that detects the current platform and installs the matching binary.

#### Scenario: Install latest
- **WHEN** the installer is run without specifying a version
- **THEN** it downloads the latest release asset for the detected OS/arch, verifies its checksum, and installs the binary

#### Scenario: Install specific version
- **WHEN** the installer is run with a version/tag
- **THEN** it downloads that release asset for the detected OS/arch, verifies its checksum, and installs the binary

