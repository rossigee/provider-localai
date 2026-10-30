# provider-localai

[![CI](https://img.shields.io/github/actions/workflow/status/rossigee/provider-localai/ci.yml?branch=master)][build]
[![Version](https://img.shields.io/github/v/release/rossigee/provider-localai)][releases]
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

[build]: https://github.com/rossigee/provider-localai/actions/workflows/ci.yml
[releases]: https://github.com/rossigee/provider-localai/releases

A [Crossplane](https://crossplane.io/) provider that manages
[LocalAI](https://localai.io/) resources through its admin API: agents, skills,
models and settings.

## Container Registry

- **Primary**: `ghcr.io/rossigee/provider-localai:v0.1.14`

## Features

- Manages `Agent`, `Skill`, `Model` and `Settings` resources
- Authenticates with a static LocalAI API key, the credential shape the rest
  of the fleet uses
- Verifies the LocalAI endpoint against a private CA rather than skipping
  verification
- Attaches MCP servers per agent
- Native controller: no Terraform, no upjet

## Getting Started

### Prerequisites

- Kubernetes cluster with Crossplane `>= v2.5`
- A LocalAI deployment with its admin API reachable from the cluster
- A LocalAI API key

### Installation

```bash
kubectl crossplane install provider ghcr.io/rossigee/provider-localai:v0.1.14
```

### Configuration

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

## Usage

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
      - example-bookkeeping
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

### Behaviour

- `Observe` reads `/api/agents/{name}/config`. The plain agent endpoint only
  reports whether an agent is active.
- A 404 deletes/creates; any other API error is surfaced as a reconcile error
  rather than treated as absence.
- The whole desired agent (model, description, system prompt, tools, MCP
  servers) is compared, so edits converge instead of being ignored.

## Resource Types

| Resource | API Group | Description |
|----------|-----------|-------------|
| Agent | `agent.localai.crossplane.io/v1alpha1` | LocalAI agents, with model, system prompt, skills and MCP servers |
| Skill | `skill.localai.crossplane.io/v1alpha1` | Skills available to agents |
| Model | `model.localai.crossplane.io/v1alpha1` | Models available to the LocalAI deployment |
| Settings | `settings.localai.crossplane.io/v1alpha1` | LocalAI instance settings |
| ProviderConfig | `localai.crossplane.io/v1beta1` | LocalAI endpoint, API key and private CA |

## Development

```bash
# Build
make go.build PLATFORMS=linux_amd64 VERSION=v0.1.14

# Test
make test

# Lint
make lint

# Generate
make generate
```

The LocalAI API client lives in
[`github.com/rossigee/localai-client`](https://github.com/rossigee/localai-client).

## Contributing

Issues and pull requests are welcome at [github.com/rossigee/provider-localai](https://github.com/rossigee/provider-localai).

## License

provider-localai is under the Apache 2.0 license.
