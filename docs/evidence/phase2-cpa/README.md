# Phase 2 CPA credential concurrency evidence

Product commit: `ee9a2665d296029874996fff0489eaa3a81c7fbf`.
Base: `49ab9afc75d0a3de11e0647f1b87659e27f03a49`.
Branch: `codex/phase2-credential-concurrency`.
Workspace: `D:/MX-CPAMP-worktrees/phase2-runtime`.

## Delivered behavior

- Every local runtime credential defaults to 20 concurrent generations across models. OAuth metadata and API-key configuration support a positive per-credential override; null/omission restores 20.
- Selection excludes otherwise-compatible credentials with no capacity. Unbound requests wait for the compatible pool, while pinned or existing affinity requests wait for their credential. Capacity skips do not consume upstream retry attempts or change credential health.
- Admission allows at most 1,000 waiters and a cumulative 30 seconds of capacity waiting. The returned local errors are 429 `credential_queue_full` and `credential_concurrency_timeout`.
- Leases cover Execute, CountTokens, SSE, management VerifyInference, Antigravity credits fallback, and Codex duplex generations. SSE cancellation cancels the upstream attempt and drains its actual source before releasing capacity. A duplex first turn uses the conductor lease; subsequent creates acquire their own reservation asynchronously. Steering keeps the parent reservation through its automatic successor, without a second charge. Required-input waits and idle sockets retain no reservation.
- Refresh and same-identity registration keep limits and active counters. Lowering a limit leaves in-flight work intact; raising it wakes waiters. Removing or replacing an identity detaches its counter so an old lease cannot decrement its replacement.
- The v8 snapshot omits secret material. Home returns only its mode and an empty list; local policy edits are rejected. Runtime/plugin credentials and keyless compatibility groups are explicitly read-only.

## Validation

Toolchain: cached Go 1.26.10 for Windows amd64, without changing global tools. Commands ran from `components/cpa` with `GOMODCACHE=C:/Users/子羽/.codex/artifacts/mx-cpamp-toolchains/gomodcache`.

| Evidence | Command/result | Code input |
| --- | --- | --- |
| `expanded-package-tests.log` | `go test ./sdk/cliproxy/auth ./sdk/cliproxy/executor ./internal/config ./internal/watcher/synthesizer ./internal/api/handlers/management ./internal/runtime/executor ./internal/api -count=1 -timeout 180s`; aggregate exit 1; first six packages passed | Same final auth, SDK, config, synthesizer and management source. The later changes were duplex admission ordering and two API tests. The failed aggregate is not recorded as a full pass. |
| `executor-final.log` | `go test ./internal/runtime/executor -count=1 -timeout 120s`; exit 0 | Final duplex ordering and generation lifecycle implementation |
| `api-targeted-final.log` | `go test ./internal/api -run '^(TestManagementV8IndependentContract\|TestServerResponsesWebsocketReauthorizesEachTurn)$' -count=1 -timeout 60s`; exit 0 | Final product and corrected tests |
| Build tool receipt | `go build -o C:/Users/子羽/.codex/artifacts/mx-cpamp-phase2-cpa.exe ./cmd/server`; exit 0 | Final product source; subsequent changes were test-only and documentation-only |
| Git tool receipt | `git diff --check` and `git diff --cached --check`; exit 0 | Product commit input |

New deterministic coverage checks default 20, busy-pool selection, pinned affinity, shared counters across lifecycle operations, lower/raise behavior, old-identity release isolation, queue exhaustion, cancellation, and a virtual-clock 30-second budget that survives notifications. Handler tests check integer bounds, null deletion, persisted overrides, secret-free snapshots, ETag conflicts, and Home ownership. Configuration tests cover all eight upstream API-key families and stable AuthIDs when only limits change.

Loopback WebSocket tests exercise the real duplex executor and source channel: first-turn handoff, automatic successor reservation transfer, required-input release, later explicit create, idle steering rejection, cancellation during a capacity wait, and steering arriving while another create is waiting for admission. No real provider login or model request was performed by this workstream.

## Failure records and focused fixes

- `initial-package-tests.log`: a new config round-trip fixture omitted the Codex base URL, so normal configuration sanitization removed its key. The fixture now uses a valid base URL; management concurrency tests pass.
- `expanded-package-tests.log`: the existing v8 route contract expected Codex OAuth import to be absent. The accepted baseline already provides that route and rejects an empty body with 400 `invalid_credentials`; the test now checks that behavior and includes the concurrency route.
- `expanded-package-tests.log` and `api-targeted-before-cleanup-fix.log`: the existing WebSocket authorization test passed its business assertions but its temporary directory was removed before the hijacked handler completed request-log cleanup. The test now joins that handler after closing the socket and server. The focused final run passes; production logging and protocol behavior are unchanged.

## Limits and next owner

No SQL migration or production data mutation. Configuration changes persist only through the existing metadata/configuration paths. No push or deployment was performed.

The Windows PATH has no GCC; the race detector was not run. The tests above are offline and loopback evidence, not production deployment or live model acceptance. The parent reviewer owns independent review and integrated release validation.
