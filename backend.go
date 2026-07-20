package cursor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/Herrscherd/herrscher-contracts"
)

// Config configures a Cursor Agent backend.
type Config struct {
	Kind     string
	Stream   bool
	Cmd      string
	Model    string
	Dir      string
	Verbose  bool
	ResumeID string // cursor session id to resume on the first turn ("" = fresh)
}

func resolveBackend(kind string, stream bool) string {
	if kind != "" {
		return kind
	}
	if stream {
		return "stream"
	}
	return "oneshot"
}

// NewBackend builds a configured Cursor Agent backend.
func NewBackend(ctx context.Context, c Config) (contracts.Backend, error) {
	kind := resolveBackend(c.Kind, c.Stream)
	if ctx == nil {
		ctx = context.Background()
	}
	base := streamBase(strings.Fields(c.Cmd))
	switch kind {
	case "oneshot":
		return &oneShotResponder{ctx: ctx, base: base, model: c.Model, dir: c.Dir, verbose: c.Verbose}, nil
	case "stream":
		return &streamResponder{ctx: ctx, base: base, model: c.Model, dir: c.Dir, verbose: c.Verbose, session: c.ResumeID}, nil
	default:
		return nil, fmt.Errorf("unknown backend kind %q", kind)
	}
}

type oneShotResponder struct {
	// ctx is the backend-lifetime context captured at NewBackend; it is used
	// only as a fallback when Respond is called with a nil ctx.
	ctx     context.Context
	base    []string
	model   string
	dir     string
	verbose bool
}

func (o *oneShotResponder) Respond(ctx context.Context, p contracts.Prompt, _ func(contracts.BackendEvent)) (string, error) {
	if ctx == nil {
		ctx = o.ctx
	}
	return runCmd(ctx, o.base, o.model, o.dir, o.verbose, p)
}

func (o *oneShotResponder) Close() error { return nil }

func runCmd(ctx context.Context, base []string, model, dir string, verbose bool, p contracts.Prompt) (string, error) {
	content := withContext(p.Context, withAttachments(p.Content, p.Attachments))
	// The prompt is delivered on stdin only; it is deliberately not passed as an
	// argv element to avoid ARG_MAX limits and exposure via /proc/<pid>/cmdline.
	argv := baseArgv(base, "json", model, "")
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(content)
	// cmd.Env left nil so the child inherits the parent environment directly.
	if verbose {
		cmd.Stderr = os.Stderr
	}
	out, err := cmd.Output()
	if err != nil {
		// stderr may carry secrets (tokens, authed URLs); never fold it into the
		// error string. Under verbose it is already streamed to os.Stderr.
		return parseResult(string(out)), fmt.Errorf("cursor exec failed: %w", err)
	}
	return parseResult(string(out)), nil
}

func parseResult(out string) string {
	var ev cursorEvent
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &ev) == nil && ev.Result != "" {
		return ev.Result
	}
	return strings.TrimSpace(out)
}

func streamBase(fields []string) []string {
	if len(fields) == 0 {
		return []string{"cursor-agent"}
	}
	return fields
}

func cursorArgv(base []string, model, resume string) []string {
	return baseArgv(base, "stream-json", model, resume)
}

// baseArgv builds the shared cursor-agent flag scaffolding. The prompt itself is
// always delivered on stdin, never as a positional argv element.
func baseArgv(base []string, format, model, resume string) []string {
	b := streamBase(base)
	argv := make([]string, 0, len(b)+7)
	argv = append(argv, b...)
	argv = append(argv, "-p", "--output-format", format)
	if model != "" {
		argv = append(argv, "--model", model)
	}
	if resume != "" {
		argv = append(argv, "--resume", resume)
	}
	return argv
}

type cursorEvent struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	IsError   bool   `json:"is_error"`
	Result    string `json:"result"`
	Message   struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	ToolCall map[string]json.RawMessage `json:"tool_call"`
}

func readTurn(r *bufio.Reader, onEvent func(contracts.BackendEvent)) (turnResult, error) {
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var ev cursorEvent
			if json.Unmarshal(line, &ev) == nil {
				switch ev.Type {
				case "assistant":
					if onEvent != nil {
						for _, b := range ev.Message.Content {
							if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
								onEvent(contracts.BackendEvent{Kind: "text", Detail: b.Text})
							}
						}
					}
				case "tool_call":
					if onEvent != nil && ev.Subtype == "started" {
						tool, detail := toolCallDetail(ev.ToolCall)
						onEvent(contracts.BackendEvent{Kind: "tool", Tool: tool, Detail: detail})
					}
				case "result":
					tr := turnResult{Text: ev.Result, SessionID: ev.SessionID, IsError: ev.IsError}
					if ev.IsError {
						tr.ErrMsg = ev.Result
					}
					if onEvent != nil {
						onEvent(contracts.BackendEvent{Kind: "result", IsError: ev.IsError})
					}
					return tr, nil
				}
			}
		}
		if err != nil {
			return turnResult{}, err
		}
	}
}

type turnResult struct {
	Text      string
	SessionID string
	IsError   bool
	ErrMsg    string
}

func toolCallDetail(call map[string]json.RawMessage) (string, string) {
	for name, raw := range call {
		var body struct {
			Args map[string]any `json:"args"`
		}
		if json.Unmarshal(raw, &body) != nil {
			return name, ""
		}
		for _, key := range []string{"command", "path", "filePath", "pattern", "query", "url", "description"} {
			if value, ok := body.Args[key].(string); ok && value != "" {
				return name, value
			}
		}
		return name, ""
	}
	return "", ""
}
