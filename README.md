# herrscher-cursor-backend

Cursor Agent backend for Herrscher. It implements `contracts.Backend` and
registers itself as the `cursor` plugin.

It uses the official Cursor Agent CLI:

```text
cursor-agent -p --output-format json
cursor-agent -p --output-format stream-json
```

`oneshot` runs one command per message. `stream` resumes the Cursor session id
between messages; Cursor's CLI exits after each headless turn, so the process is
not kept open. Both modes pass context and downloaded attachment paths in the
prompt and do not define provider-specific environment variables.

Authentication is handled by Cursor Agent itself (`cursor-agent login` or
`CURSOR_API_KEY`).

```bash
GOWORK=off go test ./...
GOWORK=off go vet ./...
GOWORK=off go test -race ./...
```
