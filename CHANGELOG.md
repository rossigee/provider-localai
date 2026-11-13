# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Breaking (Model URLs)

- A Model whose configuration contains an `http` or `https` URL is rejected
  unless the URL's host is listed in the ProviderConfig's
  `spec.allowedModelUpstreams`. The default is an empty list. This covers
  `proxy.upstream_url`, `download_files`, and URI-valued `model` or `mmproj`.
  Add the hosts to the ProviderConfig before those Models are next applied.

### Changed

- The `internal/tracing` package, which nothing imported, is removed. Its
  direct OpenTelemetry dependencies are gone from `go.mod`.
- State metrics are recorded for Agent, Model, Skill and Settings. The
  `--poll-state-metric` flag was parsed, but no resource kinds were listed, so
  nothing was recorded.

## [0.3.1] - 2026-11-13

### Breaking

- Agent, Model, Skill and Settings resolve their ProviderConfig in their own
  namespace. The provider no longer falls back to `crossplane-system`. Create
  a ProviderConfig beside each resource, or set the resource's namespace to
  the ProviderConfig's namespace.
- Secrets referenced by a ProviderConfig (`credentials.secretRef`,
  `rootCACertificateRef`, `clientSecretRef`) must be in the ProviderConfig's
  namespace. An Agent's `apiKeySecretRef` must be in the Agent's namespace.
  Other references are rejected.
- `spec.forProvider.mcpServers[].token` is no longer accepted. An Agent that
  sets it fails to connect. Move the value into a Secret in the Agent's
  namespace and set `tokenSecretRef` instead.

### Changed

- The ProviderConfig controller makes an authenticated request to LocalAI
  (`/v1/models`) and reports Ready only if it succeeds. Previously it reported
  Ready unconditionally. Resources wait for Ready before connecting, so a wrong
  or revoked API key now blocks them with a visible condition.
- Agents record the name and `resourceVersion` of each Secret their API key and
  MCP tokens come from, in `status.atProvider.secretVersions`. Rotating a
  referenced Secret updates the agent. The status holds no secret values.
  MCP token values are no longer compared with LocalAI.

- Agents, Models and Skills record `status.atProvider.ownership`. An object
  that was already in LocalAI when the resource first observed it is adopted,
  and deleting the resource leaves it in place. Objects created by the provider
  are deleted as before. Resources from before this change keep their current
  delete behaviour.

### Fixed

- A Model whose import is still queued in LocalAI is no longer re-posted on
  every poll. The job ID is kept in `status.atProvider.pendingImportUUID` and
  the import is resubmitted after 10 minutes if the model has not appeared.
- Go moves to 1.27.4, which fixes GO-2026-6859 (HTTP/1 server crash via a
  `MaxBytesReader` header race in `net/http`). The govulncheck step had been
  silently failing to run, so the finding went unreported. It now runs the
  installed scanner directly.
- Auto-merge for Dependabot PRs no longer treats a missing or unfinished
  required check as passed.
- govulncheck in CI and the security workflow fails when the scanner itself
  errors, instead of reporting "No vulnerabilities found".
- The auto-merge notification step passes PR title and labels through
  environment variables rather than interpolating them into the script.

## [0.2.2] - 2026-11-08

### Fixed

- The Model controller's raw HTTP calls (`/models/import`, `/models/delete`)
  now use the ProviderConfig's `rootCACertificateRef` and OAuth2 settings
  instead of `http.DefaultClient`, which ignored both.

## [0.2.0] - 2026-11-07

### Added

- The Model controller manages models: `Create`/`Update` write the
  configuration through `POST /models/import` (re-importing overwrites) and
  `Delete` posts to `/models/delete/{name}`. The new
  `spec.forProvider.config` field carries the full model configuration as
  YAML; drift is detected by comparing the sha256 recorded in
  `status.atProvider.configHash`, since LocalAI has no endpoint that returns
  a model's stored configuration. Requires `localai-client` v0.1.5.

### Breaking

- Agent, Model, Settings and Skill are now namespaced and served from
  `<kind>.localai.m.crossplane.io/v1beta1`. They were cluster-scoped under
  `*.localai.crossplane.io/v1alpha1`. ProviderConfig moved to
  `localai.m.crossplane.io/v1beta1`. Orphan existing resources before removing
  the old CRDs; see [docs/migration-namespaced-groups.md](docs/migration-namespaced-groups.md).

## [0.1.16] - 2026-10-26

### Changed

- Move to `github.com/rossigee/crossplane-runtime/v2` v2.5.1, the version
  the rest of the fleet is standardising on. It brings
  `sigs.k8s.io/controller-runtime` v0.25.1 and `k8s.io` v0.37.0 with it.
- The CRDs are regenerated with controller-gen v0.22.0; only the
  `controller-gen.kubebuilder.io/version` annotation changes.

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
