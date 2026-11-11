# Local credential concurrency contract

Default limit: 20 per runtime AuthID, shared across models. Missing override inherits 20. Home mode remains authoritative and does not apply local admission.

## Snapshot

`GET /v8/management/credentials/concurrency` uses existing management authentication.

```json
{"mode":"local","default_limit":20,"pool_waiting":0,"credentials":[{"auth_id":"ID","auth_index":"INDEX","name":"NAME","provider":"codex","source":"file","editable":true,"read_only_reason":"","concurrency_limit":null,"effective_limit":20,"limit_source":"default","active":0,"pinned_waiting":0}]}
```

`source` is `file`, `config`, or `runtime`. Each entry includes `editable` (boolean) and `read_only_reason` (empty for editable entries, `runtime_source`, `plugin_virtual`, or `keyless_group` otherwise). Git/objectstore/postgres persisted files use `file`; runtime-only/plugin virtual credentials use `runtime` and are read-only. OpenAI-compatible groups without a `keys[]` credential keep `source:config` and are read-only with reason `keyless_group`; their default admission limit remains 20. No upstream keys or tokens are returned. `name` is the existing credential filename, falling back to AuthID. Home returns exactly `{"mode":"home","credentials":[]}`: no default, active, or waiting counts. Concurrency edits return 409 in Home mode.

## OAuth / authfile override

`PATCH /v8/management/credentials/fields` with `{name: AUTH_ID, concurrency_limit: 20}`. A positive integer up to 2147483647 is accepted. `null` removes the override and restores the default. Invalid/fractional/nonpositive values return 400. Existing mutation and persistence apply; refresh and same-identity re-login preserve metadata.

## Config API key override

Read `/v8/management/config/api-keys`, retain ETag, locate the exact key node by `auth_index` matching the snapshot `auth_index` (existing v8 config mapping). Update only its `concurrency-limit` with the existing config PUT/PATCH and `If-Match`; omit/remove the field to inherit 20. Never edit by array position alone or return raw config keys to the browser. Changed ETag or missing/ambiguous mapping requires refetch, not guessing. `concurrency-limit` is per `keys[]` entry, not the provider group; it does not enter AuthID derivation.

## Admission

Unbound requests wait for any compatible credential; pinned/affinity requests wait for the original credential. Cumulative admission wait is at most 30 seconds per request/WS generation turn. Timeout: 429 `credential_concurrency_timeout`; 1000 total waiters exhausted: 429 `credential_queue_full`. Busy skips do not cool down credentials or consume upstream retry attempts. Active streams retain capacity to actual source completion; idle WS connections do not consume a generation slot. Lower limits do not interrupt in-flight work.
