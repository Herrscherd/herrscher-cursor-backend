# Cursor backend review — 2026-07-15

Scope: `master` at `46a1fa0`. Findings are limited to behavior verified in the
repository and the official Cursor CLI contract.

## Verification

- `GOWORK=off go test ./...` — passed
- `GOWORK=off go test -race ./...` — passed
- `GOWORK=off go vet ./...` — passed
- `GOWORK=off go test -cover ./...` — passed, 44.2% statement coverage
- `govulncheck ./...` — no vulnerabilities found
- No local working-tree changes
- No open pull request exists for this repository

## Findings

### CI compliance — gap, not a failing check

There is no repository workflow under `.github/workflows`. GitHub currently
shows only the automatic dependency-graph update, so there is no project CI
that runs the Go test, race, vet, or build gates. The commands pass locally, but
the repository does not enforce them remotely.

Recommendation: add a small Go 1.25 workflow running `go test ./...`,
`go test -race ./...`, `go vet ./...`, and `go build ./...`.

### Architecture — coherent, with one explicit product decision

The `stream` strategy is a logical Cursor session resumed with `--resume`; it
does not keep a CLI process alive. This matches the headless Cursor CLI model
and is accurately described in the README.

Cursor's non-interactive agent can require command approval. The backend does
not expose a `Force`/approval policy and does not implement `ChoiceAware`.
That is a deliberate safety choice today, but the host must choose between
safe read-only behavior and an explicit automation mode before this backend is
used for autonomous file edits.

### Performance

No confirmed regression. One Cursor process is launched per logical turn, which
is required by the chosen resume-based design. Parsing is streaming NDJSON and
does not accumulate the whole output.

### Code quality

No confirmed correctness issue. The code is small, the subprocess boundary is
clear, and the tests cover argument construction, result parsing, stream events,
plugin registration, and attachments.

### Security

No confirmed vulnerability. Commands are invoked with `exec.Command` rather
than through an implicit shell, the environment is inherited without adding
provider-specific variables, and `--force` is not enabled implicitly.

Operational note: `Cmd`, `Dir`, and Cursor's tool permissions are trusted host
configuration, not untrusted Discord input.

### Bug review

No reproducible bug found in the tested implementation. The parser ignores
unknown Cursor event types as required for forward compatibility and returns an
error when a run ends without a terminal result.

### Comments and documentation

No useless implementation comments were found. README matches the current
module name, plugin kind, CLI commands, resume-based stream behavior,
authentication options, and verification commands.

## Follow-up proposals

1. Add CI enforcement.
2. Add a `Force`/permission policy only after deciding how the host should
   expose approval and autonomous-edit behavior.
3. Add an authenticated live smoke test once `cursor-agent` is available in the
   test environment; local validation currently covers the parser and process
   contract but not the live Cursor service.
