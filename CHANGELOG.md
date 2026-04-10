# Changelog

## v0.7.0 (2026-04-10)

### Features

- animate individual SVG elements — bouncing bars, dynamic trend line, sparkles

### Bug Fixes

- regenerate animated icon with transparent background

### Other

- docs: add Andrevops ecosystem section
- docs: add icon and animated header to README

[Full changelog](https://github.com/Andrevops/claude-stats/compare/v0.6.0...v0.7.0)

## v0.6.0 (2026-04-09)

### Features

- add scrollable viewport with spinner for command output

[Full changelog](https://github.com/Andrevops/claude-stats/compare/v0.5.0...v0.6.0)

## v0.5.0 (2026-04-09)

### Features

- replace interactive menu with BubbleTea TUI

### Bug Fixes

- correct token pricing for Opus 4.5/4.6 and Haiku 4.5

[Full changelog](https://github.com/Andrevops/claude-stats/compare/v0.4.1...v0.5.0)

## v0.4.1 (2026-04-09)

### Bug Fixes

- show dates in heatmap DOW grids to prevent future-day confusion

### Other

- docs: update README with color feature descriptions

[Full changelog](https://github.com/Andrevops/claude-stats/compare/v0.4.0...v0.4.1)

## v0.4.0 (2026-04-09)

### Features

- add ANSI 256-color support for charts, heatmaps, and scorecard

[Full changelog](https://github.com/Andrevops/claude-stats/compare/v0.3.0...v0.4.0)

## v0.3.0 (2026-04-07)

### Features

- add trends command and --json export for tokens/report/trends

### Other

- chore: add MIT LICENSE file
- chore: remove Python package, moved to python-legacy branch
- docs: add detailed per-command documentation with examples

[Full changelog](https://github.com/Andrevops/claude-stats/compare/v0.2.4...v0.3.0)

## v0.2.4 (2026-04-07)

### Bug Fixes

- correct error detection and allowlist pattern matching

### Other

- docs: add update, verification, and --ai flag documentation

[Full changelog](https://github.com/Andrevops/claude-stats/compare/v0.2.3...v0.2.4)

## v0.2.3 (2026-04-07)

### Bug Fixes

- rewrite self-update as native Go with atomic same-fs replace

[Full changelog](https://github.com/Andrevops/claude-stats/compare/v0.2.2...v0.2.3)

## v0.2.2 (2026-04-07)

### Bug Fixes

- download to temp file before replacing binary during self-update
- regenerate signing key pair

[Full changelog](https://github.com/Andrevops/claude-stats/compare/v0.2.1...v0.2.2)

## v0.2.1 (2026-04-07)

### Bug Fixes

- revert action-gh-release to v2, v3 does not exist

[Full changelog](https://github.com/Andrevops/claude-stats/compare/v0.2.0...v0.2.1)

## v0.2.0 (2026-04-07)

### Features

- add ED25519 binary signing and build provenance attestation

### Bug Fixes

- invalid Docker container name in docker-install target
- upgrade GitHub Actions to Node.js 24 compatible versions

[Full changelog](https://github.com/Andrevops/claude-stats/compare/v0.1.0...v0.2.0)

## v0.1.0 (2026-04-07)

### Features

- add auto-release script with conventional commit detection
- add 'by Andrevops' subtitle to header
- rewrite as Go binary with cross-platform install
- initial project structure with pip-installable CLI

### Bug Fixes

- kill running claude-stats process before replacing binary on Windows
- locked binary on self-update, sessions off-by-one, running indicator
- remove double-width emoji from header to fix box alignment
- remove unused turnHasRead variable in efficiency command
- use dev as version fallback, real version comes from git tags
- correct GitHub URLs and add pipx install instructions

### Other

- docs: rewrite README for Go binary install paths
- Merge pull request #1 from AgusRdz/pr/go-binary

[Full changelog](https://github.com/Andrevops/claude-stats/commits/v0.1.0)
