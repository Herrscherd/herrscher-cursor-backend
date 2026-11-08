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

With `Deep`, a logged in account also runs one read-only turn
(`-p --output-format json --mode ask`, on the `model` setting when set, in the
`dir` setting), which settles a failed account lookup: a turn refused for
authentication is `down` with `health.credentials_refused` and the login remedy, a
rate limit is `degraded` with `health.rate_limited`, any other failure is `down`,
and an answered turn is `ok`. The remedy names only the binary, never the flags
of the `cmd` setting, so a key passed there is not shown.

`status` does not see a `CURSOR_API_KEY`: an install that runs on an API key
alone reports `off`, which fails no check. Cursor profiles are not supported by
the host accounts store, so only the native account is probed.

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
