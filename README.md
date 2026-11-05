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

Each turn child starts in its own process group (`Setpgid`, in `process_unix.go`).
The group is killed with `SIGKILL` as soon as the direct child exits, and again
when a turn stops early (a read error, a `oneshot` cap, or the turn context), so
a background process the CLI leaves behind dies with the turn. Output is drained
in parallel with that kill. Once the direct child has exited, a descendant that
escaped the group (for example through `setsid`) and still holds stdout gets one
second to finish; after that the read is cut short and the turn ends without
error. This package does not kill such an escaped process. Because the child is
in its own process group, a terminal Ctrl-C does not stop the child; the turn
ends when the child exits or when its context is cancelled. Outside Unix
(`process_other.go`) the group kill is absent and only the direct child is
killed.

## Compaction

The backend does not implement `contracts.Compactor` and does not declare
`Capabilities.Compactor`. The documentation describes no compaction that runs in
print mode (`-p`), so the backend declares no compaction capability. The contract
asks the host to refuse a compaction of a backend that lacks the port, before
anything is sent.

The [slash commands reference](https://cursor.com/docs/cli/reference/slash-commands)
lists `/summarize`, with `/compress` as an alias, described as "Summarize the
conversation to reduce context". That page does not say that slash commands run
in print mode. The [headless reference](https://cursor.com/docs/cli/headless)
covers `-p` with the `text`, `json` and `stream-json` formats and does not mention
slash commands or compaction.

The port is to be added when the documentation describes a compaction that a
headless caller can drive.

## Model catalog

`Models` (`models.go`) is published through `Manifest.Models`, so the host can
read this backend's catalog without instantiating it.

Every entry is route `native`. `cursor-agent` answers on the login of the local
install, and there is no way to point it at the product's gateway. A host running
under the `gateway-only` route policy therefore filters the whole catalog out and
cursor disappears from the selector entirely. That is intended, not a packaging
bug.

Prices are USD per million tokens (input, cached input, cache write, output) from
the model table at https://cursor.com/docs/models. They are Cursor's own list
prices, not the upstream provider's. A cache write is set only where the table
lists one. The `-high` variants carry the price of their base model, because
Cursor does not price by reasoning effort.

Context windows are set only where the table says "Up to 1M" (Opus 4.8, Opus 5,
Opus 5.5, Sonnet 5, Kimi K3, GPT-5.5, GPT-5.6 Sol, Terra and Luna). Every other
window is 0, which means unknown: Cursor marks them "Not published", or ties them
to a plan (Grok 4.7 is 256K on Plus).

Models are hidden when a newer model replaces them in the table (Opus 4.8, Opus 5,
Sonnet 5, Fable 5, GPT-5.2, GPT-5.3 Codex, GPT-5.5, Grok 4.5), and when Cursor
marks them "Hidden by default" (Gemini 3.7 Flash, GLM 5.2, Kimi K3).

What the catalog does not model:

- Long-context pricing. GPT-5.5 and the GPT-5.6 models cost 2x input above their
  standard rate for long context up to 1M tokens, and the page gives no threshold
  for them. Grok 4.7 costs 2x above 256K input tokens. Fast-mode and Max Mode
  rates are not listed. The contract has no tier field, so every entry carries
  the standard rate only.
- Fast variants of any model, which are separate table rows.
- The GPT-5.6 Sol promotion. Cursor states its promotional pricing (the $4 input,
  $5 cache write, $0.4 cached input and $20 output in the catalog) runs through
  21 November 2026. The page gives no standard price after that date, so the
  entry must be updated once Cursor publishes it.

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

## Windows process tree

On Windows the child tree is stopped with `taskkill /T /F`, a snapshot taken at kill
time. No Job Object is used, so a grandchild started after that snapshot, or one that
detaches before it, can outlive the backend. The Windows CI job runs the tests that do
not need a shell script, so the process-group tests stay on Unix.

## Further reading

- [Herrscher docs](https://github.com/Herrscherd/herrscher-docs), page
  `plugins/backend`
- [contracts](https://github.com/Herrscherd/herrscher-contracts), for the port
  signatures
