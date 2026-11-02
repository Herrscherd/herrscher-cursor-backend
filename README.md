# herrscher-cursor-backend

**Cursor Agent CLI as a Herrscher backend.** It turns an inbound prompt into a
reply by running the locally installed `cursor-agent` binary headless
(`-p --output-format json|stream-json`).

It is not an API client. It holds no key and reaches no Cursor endpoint itself.
Authentication belongs to your `cursor-agent` install, via `cursor-agent login`.

Category: backend, the model edge. Ports: `contracts.Backend`, and
`contracts.ResumeAware` in stream mode. Status: live.

## Install

```bash
herrscher plugin add github.com/Herrscherd/herrscher-cursor-backend
```

## Configuration

| Setting | Default | What it is |
|---|---|---|
| `CURSOR_CMD` | `cursor-agent` | the binary to run |
| `CURSOR_MODEL` | | the model a session gets when it names none |
| `CURSOR_STREAM` | `true` | `true` selects stream mode; `false`, `0` or `off` select oneshot. Any other value fails the factory at start. |
| `CURSOR_DIR` | | the working directory the CLI runs in |
| `CURSOR_KIND` | | `stream` or `oneshot`, the explicit form of `CURSOR_STREAM`; when set it takes precedence |

The `resume` setting is declared in the manifest with no environment variable.
The host feeds it at construction from the session it persisted, and an operator
does not set it.

## Two modes

`stream`, the default, uses `--output-format stream-json` and maps assistant text
blocks, `tool_call` starts and the terminal `result` event onto
`contracts.BackendEvent`.

Unlike a persistent-process backend, `cursor-agent` exits after every headless
turn, so continuity comes from the session id in the `result` event. It is stored
and replayed as `--resume` on the next turn, and exposed via `ResumeToken()`. The
host persists it and feeds it back through the `resume` setting at construction,
which is why `resume` has no environment variable.

`oneshot` (`CURSOR_STREAM=false` or `CURSOR_KIND=oneshot`) uses
`--output-format json`, emits no mid-turn events, and does not resume.

Both modes deliver the prompt on stdin only, never as argv. That prompt is the
recalled memory fenced into a `<memory data-only="true">` block, plus any
downloaded attachment paths. Neither mode defines a provider-specific environment
variable: the child inherits the parent environment as it is.

stderr is discarded unless `Verbose` is set, and it is never folded into a
returned error, because it may carry tokens. A line of stream output longer than
16 MiB ends the turn with a `LineTooLongError`. Stdout of a `oneshot` turn longer
than 16 MiB kills the child and ends the turn with an `OutputTooLargeError`, whose
output is never returned. Both limits are the same cap.

When a stream turn stops reading on a read error, the child is killed before the
turn returns, so a child that keeps running after an overlong line cannot hold the
turn open.

## Model catalog

`Models` (`models.go`) is published through `Manifest.Models`, so the host can
read this backend's catalog without instantiating it.

Every entry is route `native`. `cursor-agent` answers on the login of the local
install, and there is no way to point it at the product's gateway. A host running
under the `gateway-only` route policy therefore filters the whole catalog out and
cursor disappears from the selector entirely. That is intended, not a packaging
bug.

## Development

This module sits outside the parent `go.work`, so local commands need
`GOWORK=off`. It has two kinds of package:

- the root `cursor` package, the plugin module compiled into the herrscher host;
- `tools/commentcheck`, build tooling that refuses comments outside the
  allowed set (exported docs, known `//go:` directives, example outputs). It is
  not linked into the plugin.

`scripts/check.sh` is the gate: module tidiness, golangci-lint v2.15.0, the
comment check, a from-scratch `go generate` diff, the dash check, the race tests,
a `CGO_ENABLED=0` build and govulncheck. Run it with `GOWORK=off`.

```bash
GOWORK=off scripts/check.sh
```

## Further reading

- [Herrscher docs](https://github.com/Herrscherd/herrscher-docs), page
  `plugins/backend`
- [contracts](https://github.com/Herrscherd/herrscher-contracts), for the port
  signatures
