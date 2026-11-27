# Provider-localai API coverage

This document maps LocalAI's admin API to the Crossplane resources this provider
manages, and lists which LocalAI `/api/settings` keys the `Settings` managed
resource accepts, blocks, or excludes.

## Resource coverage

| LocalAI API | CRD | Controller | Endpoints covered | Notes |
|-------------|-----|------------|-------------------|-------|
| `/api/chats/agents` | `agents.localai.m.crossplane.io` | `internal/controller/agent` | list, get, create, update, delete | Skills/collections/MCP servers referenced by name; `spec.forProvider.owner` sends `user_id` for admin impersonation |
| `/api/chats/skills` | `skills.localai.m.crossplane.io` | `internal/controller/skill` | list, get, create, update, delete | Layered skill writes |
| `/api/collections` + `/sources` | `collections.localai.m.crossplane.io` | `internal/controller/collection` | list collections, create, add/remove sources | |
| `/api/chats/projects` (+ archive/restore) | `projects.localai.m.crossplane.io` | `internal/controller/project` | list, get, create, update, delete, archive, restore | `Archived *bool` |
| `/api/models` (capabilities, enabled, pinned, import, delete) | `models.localai.m.crossplane.io` | `internal/controller/model` | list, get, create, update, delete | Enabled/pinned/gallery install |
| `/models/apply` + `/models/jobs/` | `models.localai.m.crossplane.io` | `internal/controller/model` | gallery install via `spec.gallery` | `PendingImportUUID` tracked |
| `/api/agent/tasks` | `agenttasks.localai.m.crossplane.io` | `internal/controller/agenttask` | list, get, create, update, delete | |
| chat triggers | `triggers.localai.m.crossplane.io` | `internal/controller/trigger` | list, get, create, update, delete | |
| `/api/backends` + install/delete/jobs | `backends.localai.m.crossplane.io` | `internal/controller/backend` | list, install, delete, job status | `PendingJobID` tracked |
| `/api/settings` | `settings.localai.m.crossplane.io` | `internal/controller/settings` | GET + POST (allow-listed keys only) | See settings coverage below |
| Provider endpoint/key/CA | `providerconfigs.localai.m.crossplane.io` | `internal/controller/providerconfig` | — | Connection credentials |

Every resource method exposed by the pinned [`github.com/rossigee/localai-client`](https://github.com/rossigee/localai-client) version has a matching CRD and controller with full Create/Observe/Update/Delete. `Agent` and `Project` resources can set `spec.forProvider.owner` to send `user_id` to LocalAI when an administrator manages another user's objects.

## Settings coverage — `POST /api/settings` allow-list

`Settings` writes are allow-list driven: any key not listed below is rejected with
`settings value not allowed`. LocalAI merges the given keys into its saved
settings, so unset keys are left unchanged.

### Allowed without type validation (`allowedKeys`)

These keys accept whatever JSON type the user writes (LocalAI is tolerant of
e.g. numeric strings there):

- `watchdog_enabled`, `watchdog_idle_enabled`, `watchdog_busy_enabled`
- `watchdog_idle_timeout`, `watchdog_busy_timeout`
- `max_active_backends`
- `force_eviction_when_busy`
- `lru_eviction_max_retries`, `lru_eviction_retry_interval`
- `vram_budget`, `vram_persistent_cache`
- `threads`, `context_size`
- `f16`
- `artifact_download_concurrency`
- `max_concurrent_backend_operations`, `max_concurrent_model_operations`
- `debug`

### Typed settings (validated)

| Key | Type | Constraint |
|-----|------|------------|
| `size_aware_eviction` | boolean | must be JSON `true`/`false` |
| `memory_reclaimer_enabled` | boolean | must be JSON `true`/`false` |
| `distributed_disk_headroom_check` | boolean | must be JSON `true`/`false` |
| `home_page_area_enabled` | boolean | must be JSON `true`/`false` |
| `shared_pages_enabled` | boolean | must be JSON `true`/`false` |
| `user_page_shares_enabled` | boolean | must be JSON `true`/`false` |
| `memory_reclaimer_threshold` | number | 0.0–1.0 fraction |
| `tracing_max_items` | integer | non-negative |
| `tracing_max_body_bytes` | integer | non-negative |
| `watchdog_interval` | duration string | non-negative Go duration, e.g. `30s`, `0` |
| `open_responses_store_ttl` | duration string | non-negative Go duration, e.g. `1h`, `0` |

### Chats keys (tool gating, kept out of `allowedKeys`)

| Key | Accepted values |
|-----|-----------------|
| `chats_tool_search` | `auto`, `on`, `off` |
| `chats_localai_admin` | `false` only (can close admin tools, never open them) |

Every other `chats_*` key is rejected.

### Branding keys (instance name, tagline, logo/favicon)

| Key | Kind | Validation |
|-----|------|------------|
| `instance_name` | text | string, ≤256 chars, no control characters |
| `instance_tagline` | text | string, ≤256 chars, no control characters |
| `logo_file` | filename | plain basename (no `/`, `\`, `.`, `..`), ≤128 chars, empty resets to default |
| `logo_horizontal_file` | filename | same as `logo_file` |
| `favicon_file` | filename | same as `logo_file` |

### Blocked keys (rejected with a specific message)

| Key | Reason |
|-----|--------|
| `auto_upgrade_addons` | set on the add-on owner, not through `/api/settings` |

### Excluded keys (not allow-listed, rejected generically)

These LocalAI settings keys exist but are intentionally **not** exposed because
they control credentials, reach remote hosts, or gate tools:

- `api_keys`, `csrf`, `cors`, `cors_allow_origins` — authentication / cross-origin access
- `galleries`, `autoload_*` — make LocalAI fetch remote content
- `p2p_*`, `federated`, `mcp_connectors_allow_private` — open a network
- `chats_*` (other than the two above), `client_jobs_allow_all_tools` — enable tools

If you need one of these exposed, file an issue and the maintainer can add it to
the allow-list with an appropriate validator after a security review.

## What counts as "coverage complete"

A LocalAI resource is considered covered when it has:
1. a CRD under `package/crds/`,
2. a controller under `internal/controller/<resource>/` with Create/Observe/Update/Delete,
3. registration in `apis/apis.go` (`AddToScheme`),
4. generation verified by `make generate` (CI `check-diff` job).

All ten resources above satisfy (1)–(4). The only surface that is intentionally
partial is `Settings` /api/settings keys, as documented above.
