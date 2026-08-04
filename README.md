# herrscher-cursor-backend

**Cursor Agent CLI as a Herrscher backend.** Turns an inbound prompt into a reply
by running the locally installed `cursor-agent` binary headless
(`-p --output-format json|stream-json`). It is not an API client: it holds no key
and reaches no Cursor endpoint itself; authentication belongs to your
`cursor-agent` install (`cursor-agent login`).

## Role · Category · Ports · Config · Status · Repo

| Aspect | Value |
|--------|-------|
| **Role** | Answers one prompt per turn by driving the local Cursor Agent CLI. |
| **Category** | Backend (model edge) |
| **Ports implemented** | `contracts.Backend`; `contracts.ResumeAware` (stream mode only) |
| **Config & env** | `CURSOR_CMD` (default: `cursor-agent`), `CURSOR_MODEL`, `CURSOR_STREAM` (default: `true`), `CURSOR_DIR`, `CURSOR_KIND` |
| **Status** | live |
| **Repo** | [herrscher-cursor-backend](https://github.com/Herrscherd/herrscher-cursor-backend) |

## Install

```bash
herrscher plugin add github.com/Herrscherd/herrscher-cursor-backend
```

## Two modes

`stream` (default) uses `--output-format stream-json` and maps assistant text
blocks, `tool_call` starts and the terminal `result` event onto
`contracts.BackendEvent`. Unlike a persistent-process backend, `cursor-agent`
exits after every headless turn, so continuity comes from the session id in the
`result` event: it is stored and replayed as `--resume` on the next turn, and
exposed via `ResumeToken()`. The host persists it and feeds it back through the
`resume` setting at construction (host-injected — it is not one of the declared,
env-backed settings).

`oneshot` (`CURSOR_STREAM=false` or `CURSOR_KIND=oneshot`) uses
`--output-format json`, emits no mid-turn events, and does not resume.

Both modes deliver the prompt — recalled memory fenced into a `<memory
data-only="true">` block, plus any downloaded attachment paths — on stdin only,
never as argv, and define no provider-specific environment variables: the child
inherits the parent environment as-is. stderr is discarded unless `Verbose` is
set, and is never folded into returned errors (it may carry tokens).

## Model catalog

`Models` (`models.go`) is published through `Manifest.Models`, so the host can
read this backend's catalog without instantiating it. Every entry is route
`native`: `cursor-agent` answers on the login of the local install and there is
no way to point it at the product's gateway. A host running under the
`gateway-only` route policy therefore filters the whole catalog out, and cursor
disappears from the selector entirely — that is intended, not a packaging bug.

## Development

This module sits outside the parent `go.work`, so local commands need
`GOWORK=off`.

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

## Further reading

- [Herrscher docs](https://github.com/Herrscherd/herrscher-docs) — `plugins/backend`
- [contracts](https://github.com/Herrscherd/herrscher-contracts) — port signatures
