# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.15] - 2026-10-26

### Changed

- Require `github.com/rossigee/localai-client` v0.1.2, which uses
  `golang.org/x/oauth2` v0.37.0 instead of v0.36.0.
- The `build` submodule moves to the head of `rossigee/build@rossigee-lint-fixes`
  (8b2ef29), which reports the advisories govulncheck found instead of only
  a count.

## [0.1.14] - 2026-10-26

### Fixed

- Build and scan with Go 1.27.3. Go 1.27.1's standard library has twelve
  known advisories (`crypto/x509` GO-2026-6809, `net/http`, `net/http/internal/http2`,
  `net/textproto`, `crypto/tls`, `html/template`) and Go 1.27.2 still carries
  GO-2026-6809. They are what made the `check-diff` job fail on every commit,
  including the ones that never touched this code. `go.mod` requires 1.27.3
  as well, so the provider binary is always linked against a patched standard
  library.
- `securego/gosec@master` and `trufflesecurity/trufflehog@main` are pinned to
  v2.30.0 and v3.99.7. Branch references make a build change without a
  commit and leave the version that ran unrecorded.

### Changed

- Require `github.com/rossigee/localai-client` v0.1.1, which requires Go
  1.27.3, uses `golang.org/x/oauth2` v0.36.0 instead of v0.24.0, and carries
  its own CI and tests.
- The `reviewable` target prints the advisories govulncheck found instead of
  only a count, and the `check-diff` job dumps the findings when it fails.
- The Makefile's `GO_REQUIRED_VERSION` moves to 1.27.3, which is the version
  the crossplane-providers audit expects.

## [0.1.13] - 2026-10-25

### Fixed

- `golang.org/x/net` v0.58.0 -> v0.60.0, which fixes the HTTP/2 framing and
  memory exhaustion advisories govulncheck reports against the version
  pulled in by controller-runtime and the OpenTelemetry exporter
  (GO-2026-6610, GO-2026-6603 and related). The matching `golang.org/x/mod`,
  `x/sync`, `x/sys`, `x/term` and `x/text` versions moved with it.

### Changed

- `gofmt` the three files the linter rejected: import ordering in
  `internal/clients/localai.go` and `internal/controller/settings/settings.go`
  and the field alignment in `internal/controller/agent/agent.go`. Whitespace
  only.

## [0.1.12] - 2026-10-25

Repository cleanup. No API changes and no change in behaviour on the cluster.

### Removed

- Dead Keycloak client code carried over from the provider this repository was
  scaffolded from: `internal/clients/{keycloak,connector,config,singleton}.go`,
  the unregistered `deletecomplete` and `mappingreconcile` controllers, and the
  `github.com/rossigee/provider-keycloak` module dependency.
- Keycloak documentation, examples and changelog, which described a different
  provider.

### Changed

- Depend on `github.com/rossigee/localai-client` v0.1.0 instead of a `replace`
  directive pointing at a local path, so the repository builds anywhere.
- `VERSION` reflects the released images instead of the scaffold's `v0.21.0`.
- README documents the LocalAI resources, credential shape and agent
  behaviour.

## [0.1.11] and earlier

Pre-release builds of the provider: agent, skill, model and settings
controllers; API-key credentials; private CA support; config-endpoint reads and
whole-agent drift comparison. Only v0.1.12 and later are supported.
