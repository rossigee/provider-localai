# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
