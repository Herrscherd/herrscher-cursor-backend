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

Building the module needs the Go toolchain go1.27.4: `go.mod` pins it with
`toolchain go1.27.4` for the GO-2026-6859 advisory on `net/http`, and a `go`
command older than that fetches it on first use.

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

`stream`, the default, uses `--output-format stream-json` and maps assistant
text blocks, `tool_call` starts and finishes, and the terminal `result` event
onto `contracts.BackendEvent`. A call's start is a `tool` event whose `ToolCall`
is in the `started` state, under the `call_id` of the line. Its end is a
`tool_done` event in the `succeeded` state, or in the `failed` state with an
`Error` of at most 200 runes. For a `shellToolCall` the error reads `exit code
N`, followed by the first line of stderr in parentheses when there is one; a
timeout reads `timeout after Nms`, a rejection `rejected: <reason>`, a spawn
failure `spawn error: <error>` and a denial `permission denied: <error>`. A
rejection, a spawn failure or a denial with no text reads as the name of its
result, as a timeout or a failure without its number does. Any other failure
takes the first non-empty of `message`, `error`, `stderr` or
`reason`, each read on its own, so a field of the wrong JSON type is skipped
and the others still count. A bare string result is taken as it is, and
without any of them the error is the name of the result. A `shellToolCall`
reads `exitCode` and `timeoutMs` only as whole numbers in the int32 range, and
`timeoutMs` only when it is not negative. A string, a fraction, an exponent,
`-0`, a value out of range or a missing value falls back to the name of the
result, and the value itself is never quoted.

`Error` is therefore the first line of stderr or of a message, and it can carry
a secret, as `Detail` can. The contract requires every exporter on the host side
to send `Error`, like `Detail`, only when the operator opts in.

A finished line whose start the turn never saw emits nothing. A finished line
whose result is absent or unreadable emits nothing either: its call stays
`started`, with no `tool_done`, and that finish still frees the call's place.

Each turn keeps two sets of `call_id`, and only a start touches them. The open
set holds the calls that started and have not finished, up to 256. The seen set
holds every ID a start has presented, up to 4096. An ID enters the seen set as
soon as its start arrives, whether that start opens a call, is refused by the
open bound, or names no tool. A `started` line opens a call only when its ID is
at most 256 bytes long, not seen yet, the open set is under 256 and the seen set
is under 4096. An ID over 256 bytes is never recorded, so its start stays bare
and its finish emits nothing. Otherwise the start leaves as a plain `tool` event
with no `ToolCall`, and the open set is left alone. A start refused by the open
bound is still recorded as seen, so it stays a plain `tool` event for the rest
of the turn, even after a place frees up. A start past the seen bound is not
recorded at all, and its finish emits nothing. The first start refused by each
bound in a turn logs one `slog.Warn` per bound and per turn, with the attributes
`bound` (`max_open` or `max_seen`) and `limit`. A finish emits only for an ID in
the open set: it frees that place, and the ID stays seen. A repeated or unknown
finish emits nothing. A call still open when the turn ends, whether on its
result, on an error or on an interruption, stays `started` with no `tool_done`:
the sets belong to one turn and are dropped with it. A `tool` line with no
`call_id` keeps only the legacy `Tool` and `Detail` fields, with no `ToolCall`.
Each assistant text block after the first non-blank text block of a turn carries
a leading blank line (`\n\n`), so consumers that concatenate text events keep
the blocks apart. Unlike the claude backend, which trims each block's text, this
backend emits the text as it arrives: a block that ends with a newline, followed
by another block, gives three line breaks at the join.

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
(`process_other.go`) there is no process group: the child and its descendants
are killed as a tree with `taskkill /T /F`; when `taskkill` fails, only the
direct child is killed.

## Health and limits

`Plugin.Probe` checks the login of the native account without a live backend and
without a turn. It runs `cursor-agent status --format json`, which reads the
stored tokens and asks Cursor who they belong to (`GetMe`, about half a second).
The command is the `cmd` setting, else `CURSOR_CMD` from the account
environment, else `cursor-agent`.

| What `status` says | State | Detail | Remedy |
|---|---|---|---|
| authenticated, with an email | `ok` | `health.signed_in_as` (the email) | |
| authenticated, without an email | `ok` | `health.connected` | |
| authenticated, but the account lookup failed | `degraded` | `health.unreachable` (`account check failed`) | run `cursor-agent login` |
| not logged in, or a refresh token missing | `off` | `health.not_logged_in` | run `cursor-agent login` |
| the binary is not installed | `off` | `health.unreachable` (`command not found`) | |
| the deadline passed | `degraded` | `health.unreachable` (`timeout`) | |
| a failing exit, or an output it cannot read | `down` | `health.unreachable` (the exit status, or `unreadable status`) | |
| a named profile is asked for | `off` | `health.unreachable` (`profiles not supported`) | |

With `Deep`, a logged in account also runs one read-only turn
(`-p --output-format json --mode ask`, on the `model` setting when set, in the
`dir` setting), which settles a failed account lookup: a turn refused for
authentication is `down` with `health.credentials_refused` and the login remedy, a
rate limit is `degraded` with `health.rate_limited`, a `dir` setting that does not exist is
`down` (`working directory missing`), any other failure is `down`, and an
answered turn is `ok`. The remedy names only the binary, never the flags
of the `cmd` setting, so a key passed there is not shown.

`status` does not see a `CURSOR_API_KEY`: an install that runs on an API key
alone reports `off`, which fails no check. Cursor profiles are not supported by
the host accounts store, so only the native account is probed. The Windows job does not run the probe
tests, which use a fake `sh` binary. The Windows job runs the deadline test, which
reaches `taskkill /T /F` without asserting that the tree died.

`Plugin.Limits` is nil: `cursor-agent` exposes no usage limit without a turn
(`about` gives the plan name, not the quota).

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
lists one. The write prices of Opus 5, Opus 4.8 and Sonnet 5 are in that table,
and they match Anthropic's 5-minute cache write price (6.25, 6.25 and 2.5). The
`-high` variants carry the price of their base model, because Cursor does not
price by reasoning effort.

Context windows are 1M where the table says "Up to 1M" (Opus 4.8, Opus 5, Opus 5.5,
Sonnet 5, Kimi K3, GPT-5.5, GPT-5.6 Sol, Terra and Luna) and for Fable 5.1, where
the table says "Not stated" but Anthropic's model page gives 1M tokens. Every other
window is 0, which means unknown: Cursor marks them "Not published", or ties them
to a plan (Grok 4.7 is 256K on Plus).

Models are hidden when a newer model replaces them in the table (Opus 4.8, Opus 5,
Sonnet 5, Fable 5, GPT-5.2, GPT-5.3 Codex, GPT-5.5, Grok 4.5), and when Cursor
marks them "Hidden by default" (Gemini 3.7 Flash, GLM 5.2, Kimi K3).

What the catalog does not model:

- Long-context pricing. GPT-5.5 and the GPT-5.6 models cost 2x input above their
  standard rate for long context up to 1M tokens, and the page gives no threshold
  for them. Grok 4.7 costs 2x standard rates above 256K input tokens, up to 500K.
  The contract has no tier field, so every entry carries the standard rate only.
- Fast variants of any model, which are separate rows of the table. Their rates
  are not modelled.
- The GPT-5.6 Sol promotion. Cursor states its promotional pricing (the $4 input,
  $5 cache write, $0.4 cached input and $20 output in the catalog) runs through
  21 November 2026. The page gives no standard price after that date, so the
  entry must be updated once Cursor publishes it.
- The Grok 4.7 500k variants, which the table lists as separate rows: Grok 4.7 500k
  at $4 input, $1 cached input and $12 output, and Grok 4.7 500k (Fast) at $6, $1.5
  and $18. The catalog models only the Grok 4.7 row.
- Sonnet 5.5 and Grok 4.6. The table lists Sonnet 5.5 at $2 input, $0.1 cached
  input, $2.5 cache write, $10 output and a 1M context, and Grok 4.6 at $2, $0.5 and
  $6 with no window. Neither is in this catalog, because their `cursor-agent` IDs
  could not be confirmed: `cursor-agent models` needs a login to list them. Both are
  added once their IDs are confirmed.

## Development

This module sits outside the parent `go.work`, so local commands need
`GOWORK=off`. They run on the toolchain `go.mod` pins, go1.27.4. It has two
kinds of package:

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
detaches before it, can outlive the backend. The Windows CI job runs a named subset of
the tests (the `-run` pattern in `.github/workflows/ci.yml`), none of which
spawns a shell script. The process-group and probe tests stay on Unix.

## Further reading

- [Herrscher docs](https://github.com/Herrscherd/herrscher-docs), page
  `plugins/backend`
- [contracts](https://github.com/Herrscherd/herrscher-contracts), for the port
  signatures
