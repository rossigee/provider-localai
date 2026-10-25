# provider-localai

[![CI](https://github.com/rossigee/provider-localai/actions/workflows/ci.yml/badge.svg?branch=master)][ci]
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

A [Crossplane](https://crossplane.io/) provider that manages
[LocalAI](https://localai.io/) resources through its admin API: agents, skills,
models and settings.

[ci]: https://github.com/rossigee/provider-localai/actions/workflows/ci.yml

## Resources

| Kind | Scope | API |
|---|---|---|
| `Agent` | Cluster | `agent.localai.crossplane.io/v1alpha1` |
| `Skill` | Cluster | `skill.localai.crossplane.io/v1alpha1` |
| `Model` | Cluster | `model.localai.crossplane.io/v1alpha1` |
| `Settings` | Cluster | `settings.localai.crossplane.io/v1alpha1` |
| `ProviderConfig` | Namespaced | `localai.crossplane.io/v1beta1` |

## Install

```yaml
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-localai
spec:
  package: ghcr.io/rossigee/provider-localai:v0.1.12
```

## Authentication

LocalAI's API accepts only API keys: the legacy `LOCALAI_API_KEY` value, a
per-user key, or a browser session ID. An OIDC access token is rejected with
`An authentication key is required`, so the provider authenticates with a
static API key read from a Secret, the same credential shape the other
providers in this fleet use.

```yaml
apiVersion: v1
kind: Secret
metadata:
  namespace: crossplane-system
  name: localai-provider-credentials
type: Opaque
stringData:
  token: "<LOCALAI_API_KEY>"
---
apiVersion: localai.crossplane.io/v1beta1
kind: ProviderConfig
metadata:
  name: localai
spec:
  baseURL: https://localai.example.com
  credentials:
    source: Secret
    secretRef:
      namespace: crossplane-system
      name: localai-provider-credentials
      key: token
  # Trust a private CA. Never set TLSInsecureSkipVerify.
  rootCACertificateRef:
    namespace: crossplane-system
    name: localai-ca
    key: ca.crt
```

`tokenURL`, `clientID` and `clientSecretRef` still exist but are deprecated.
Use them only when LocalAI sits behind something that accepts OAuth2 bearer
tokens.

## Agents

```yaml
apiVersion: agent.localai.crossplane.io/v1alpha1
kind: Agent
metadata:
  name: bookkeeping
spec:
  forProvider:
    name: bookkeeping
    model: qwen3-8b
    description: Bookkeeping via Odoo MCP only.
    systemPrompt: |
      You keep the books. Use only the Odoo MCP tools.
    enableSkills: true
    skills:
      - golder-bookkeeping
    mcpServers:
      - url: http://odoo-mcp.localai.svc.cluster.local:8080/stream
    apiKeySecretRef:
      namespace: crossplane-system
      name: localai-provider-credentials
      key: token
  providerConfigRef:
    name: localai
```

`apiKeySecretRef` is required when LocalAI has authentication enabled. LocalAI
only mints a per-agent key for callers that have a row in its `users` table,
and a provider using a static API key has none, so creation would otherwise
fail with `fk_user_api_keys_user`. Supplying the key skips minting.

Agents do not inherit model-level `mcp.remote` settings; list MCP servers per
agent.

### Skills

`spec.forProvider.skills` is sent as `selected_skills`. LocalAI builds that bind
LocalAGI's `state.AgentConfig` have no such field and drop it on write, which
leaves every skill available to the agent. Because the value cannot be read
back, `skills` is excluded from the drift comparison; it is still sent, so a
LocalAI build that persists it gets it.

## Behaviour

- `Observe` reads `/api/agents/{name}/config`. The plain agent endpoint only
  reports whether an agent is active.
- A 404 deletes/creates; any other API error is surfaced as a reconcile error
  rather than treated as absence.
- The whole desired agent (model, description, system prompt, tools, MCP
  servers) is compared, so edits converge instead of being ignored.

## Building

```bash
make go.build PLATFORMS=linux_amd64 VERSION=v0.1.12
make do.build.images PLATFORMS=linux_amd64 VERSION=v0.1.12
go test ./...
```

The LocalAI API client lives in
[`github.com/rossigee/localai-client`](https://github.com/rossigee/localai-client).

## License

Apache License 2.0. See [LICENSE](LICENSE).
