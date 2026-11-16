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

- **Primary**: `ghcr.io/rossigee/provider-localai:v0.1.18`

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
kubectl crossplane install provider ghcr.io/rossigee/provider-localai:v0.1.18
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
apiVersion: localai.m.crossplane.io/v1beta1
kind: ProviderConfig
metadata:
  name: localai
  namespace: crossplane-system
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

### Namespaces

Agent, Model, Skill and Settings resolve `spec.providerConfigRef.name` to a
ProviderConfig in their own namespace. A resource therefore needs a
ProviderConfig beside it; the provider does not fall back to
`crossplane-system`.

Every Secret a ProviderConfig references (`credentials.secretRef`,
`rootCACertificateRef`, `clientSecretRef`) must be in the ProviderConfig's
namespace. A reference to any other namespace is rejected, so a tenant cannot
make the provider read a Secret it could not read itself.

### Identity and deletion

Agents, skills and models are owned by the service user behind the
ProviderConfig's API key. A resource only manages what it creates. An Agent
records its LocalAI ID as the `crossplane.io/external-name` annotation and
uses that ID for every later call. A Skill is identified by its `layer` and
`name`. Deleting the resource deletes the LocalAI object; an object that is
already gone is not an error.

### Triggers

A Trigger is a LocalAI chat trigger: a prompt that runs on a schedule, through a
saved agent or a model. The name, prompt, schedule kind and time, daily cap,
agent, model and enabled flag are managed. The trigger's LocalAI ID is its
external name. Only the `schedule` kind is managed. Deleting the resource deletes
the trigger.

### Agent tasks

An AgentTask is a saved LocalAI agent task. Its name, description, model, agent
ID, prompt and enabled flag are managed; the provider reads them back and applies
changes. The task's LocalAI ID is its external name. Deleting the resource deletes
the task; its past jobs stay until LocalAI's retention passes.

Cron schedules, multimedia sources and webhooks are not managed. LocalAI rejects
cron on a task (schedule a saved agent with a trigger instead), and webhook
headers come back hidden, so their values cannot be compared.

### Collections

A Collection is a LocalAI knowledge-base collection. `spec.forProvider.sources`
lists the URLs it follows. The provider keeps exactly those URLs: a URL it
does not list is stopped on the next reconcile. `updateIntervalMinutes` is
compared only when it is set.

LocalAI has no endpoint that deletes a collection. Deleting the resource stops
the URLs listed in its spec, and the collection and its entries stay in LocalAI.
Files uploaded to a collection are not managed by this resource.

### Model upstreams

A Model configuration can make LocalAI fetch URLs (`download_files`, URI-valued
`model` or `mmproj`) and send requests to a proxy upstream (`proxy.upstream_url`).
LocalAI would also use an environment variable named by `proxy.api_key_env`
for those requests. A Model that could choose these destinations could send
LocalAI's credentials to a host of its choosing.

So the ProviderConfig lists the hosts a Model may use in
`spec.allowedModelUpstreams`. Any `http` or `https` URL in a Model configuration
must name a listed host, or the Model is rejected before LocalAI is called. An
entry is a host, or `host:port` to require a port. The default is an empty list,
which allows no URLs. The `examples/provider/config.yaml` lists `opencode.ai`
for the example Model.

### Runtime settings

A Settings resource applies LocalAI runtime settings through `POST /api/settings`.
`spec.forProvider.settings` is a JSON object. LocalAI merges it into its saved
settings, so only the keys given change. `examples/settings.yaml` shows one.

Only allow-listed keys are accepted. The list is in `internal/controller/settings`.
It covers watchdog, VRAM, eviction, thread and context tuning, and `debug`.
Keys that control authentication or cross-origin access (`api_keys`, `csrf`,
`cors`, `cors_allow_origins`), make LocalAI fetch remote content (`galleries`,
`autoload_*`), open a network (`p2p_*`, `federated`), or enable tools (`chats_*`)
are rejected.

On each observe the provider reads the managed keys back from `GET /api/settings`
and compares them with the spec. A value changed in LocalAI, or a managed key
LocalAI does not report, marks the resource out of date and the settings are
applied again. Keys the spec does not set are not compared.

The provider records a hash of the settings it last applied in
`status.atProvider.appliedHash`. A change to `spec.forProvider.settings` posts
the settings again. Settings changed directly in LocalAI are not detected,
because the provider does not read settings back. Deleting the resource does
not revert LocalAI's settings.

## Usage

```yaml
apiVersion: agent.localai.m.crossplane.io/v1beta1
kind: Agent
metadata:
  name: bookkeeping
  namespace: crossplane-system
spec:
  forProvider:
    name: bookkeeping
    model: qwen3-8b
    description: Bookkeeping via Odoo MCP only.
    instructions: |
      You keep the books. Use only the Odoo MCP tools.
    skills:
      - example-bookkeeping
    collections:
      - books
    mcpServers:
      - odoo
    network: off
  providerConfigRef:
    name: localai
```

The agent's name is the LocalAI name. `skills`, `collections` and `mcpServers`
are lists of those names, and `addons` takes add-on IDs. MCP servers are named
as they are configured in the model's MCP configuration, so no URL or token
is set on the agent.

Changes to any field are pushed on the next reconcile. A folder is set with
`folder.path` and an optional `folder.mode`.

Agents and skills belong to the user behind the API key. Use an API key issued
to a dedicated service user so provider-managed agents stay separate from
people's own agents.

### Skills

A skill is identified by `layer` (`personal` or `shared`) and `name`. Its
`content` is sent as the skill body. `Observe` compares the description and
body, so an edit is pushed on the next reconcile.

### Behaviour

- Agents are read at `/api/chats/agents/{id}`; skills at
  `/api/chats/skills/{layer}/{name}`.
- A 404 means the object is absent; any other API error is surfaced as a
  reconcile error rather than treated as absence.
- An Agent whose external name is empty does not exist yet and is created.

### Model

`spec.forProvider.gallery` installs the model from a LocalAI gallery entry, by
`id` and an optional `variant`, under the Model's name. The provider polls the
install job, and a failed install is reported and retried on the next reconcile.
`gallery` and `config` cannot both be set. Gallery models are not re-installed
on update; only `enabled` and `pinned` are applied.

`spec.forProvider.enabled` and `spec.forProvider.pinned` keep a model enabled
or disabled, and pinned or unpinned, in LocalAI. A flag the spec leaves unset is
not managed. The flags are read from `/api/models/capabilities` and applied after
the config import.

- `Create`/`Update` POST the configuration to `/models/import`, which writes
  `/models/<name>.yaml`. Re-importing overwrites, so both operations are the
  same call; both are synchronous unless the config references downloads
  (`download_files`, URI-valued `model`/`mmproj`), in which case LocalAI
  queues a gallery job and `Observe` reports existence once it lands. The job
  ID is kept in `status.atProvider.pendingImportUUID`. While the model is not
  yet listed, `Observe` reports it as existing and current rather than posting
  the import again. If it has not appeared after 10 minutes, the import is
  treated as failed and submitted again.
- `Delete` posts to `/models/delete/{name}` and returns immediately; the
  model leaves the list from `/v1/models` when the job completes, which
  `Observe` sees.
- Changes made to a model in LocalAI outside this provider are not detected.
  LocalAI has no endpoint that returns a model's stored configuration, so the
  provider can only track the spec it last applied.
- There is no endpoint that returns a model's stored configuration, so drift
  detection compares the spec: the sha256 of the canonical desired config is
  recorded in `status.atProvider.configHash`. The first observation of an
  already-existing model adopts the desired hash as the baseline without
  rewriting the external config; later spec edits surface as
  `ResourceUpToDate=false` and converge via `Update`.
- `spec.forProvider.config` is the full model configuration as YAML (the
  shape LocalAI stores). The CR `name` and `backend` are authoritative:
  a config whose name or backend disagrees is rejected.

## Resource Types

| Resource | API Group | Description |
|----------|-----------|-------------|
| Agent | `agent.localai.m.crossplane.io/v1beta1` | LocalAI agents, with model, instructions, skills, collections and MCP servers |
| Skill | `skill.localai.m.crossplane.io/v1beta1` | Skills available to agents |
| Collection | `collection.localai.m.crossplane.io/v1beta1` | Knowledge-base collections that follow URLs |
| AgentTask | `agenttask.localai.m.crossplane.io/v1beta1` | Saved agent tasks: prompt, model, agent and enabled flag |
| Trigger | `trigger.localai.m.crossplane.io/v1beta1` | Chat triggers that run a prompt on a schedule |
| Model | `model.localai.m.crossplane.io/v1beta1` | Models available to the LocalAI deployment |
| Settings | `settings.localai.m.crossplane.io/v1beta1` | LocalAI instance settings |
| ProviderConfig | `localai.m.crossplane.io/v1beta1` | LocalAI endpoint, API key and private CA |

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
