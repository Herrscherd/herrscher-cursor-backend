package cursor

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// ErrInvalidSetting reports a setting whose value the factory cannot read.
var ErrInvalidSetting = errors.New("invalid cursor setting")

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:     "cursor",
			Category: contracts.CategoryBackend,
			Status:   contracts.StatusLive,
			Config: []contracts.Setting{
				{Key: "cmd", Env: "CURSOR_CMD", Help: "base command to run Cursor Agent", Default: "cursor-agent"},
				{Key: "model", Env: "CURSOR_MODEL", Help: "model override"},
				{Key: "stream", Env: "CURSOR_STREAM", Help: "resume-based stream mode (false, 0 or off to disable)", Default: "true"},
				{Key: "dir", Env: "CURSOR_DIR", Help: "working directory"},
				{Key: "kind", Env: "CURSOR_KIND", Help: "backend kind, oneshot or stream"},
				{Key: "resume", Help: "session id to resume, fed by the host at construction, never set by an operator"},
			},
			Models: Models(),
			Capabilities: contracts.Capabilities{
				Gate:    contracts.GrainNone,
				GateWhy: "cursor-agent exposes no permission hook",
			},
		},
		Discover: discover,
		Backend: func(ctx context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
			kind, err := backendKindSetting(cfg)
			if err != nil {
				return nil, err
			}
			stream, err := streamSetting(cfg)
			if err != nil {
				return nil, err
			}
			return NewBackend(ctx, Config{
				Kind:     kind,
				Stream:   stream,
				Cmd:      cfg.Get("cmd"),
				Model:    cfg.Get("model"),
				Dir:      cfg.Get("dir"),
				ResumeID: cfg.Get("resume"),
			})
		},
	})
}

const defaultStream = true

func streamSetting(cfg contracts.PluginConfig) (bool, error) {
	text := strings.TrimSpace(cfg.Get("stream"))
	if text == "" {
		return defaultStream, nil
	}
	if strings.EqualFold(text, "off") {
		return false, nil
	}
	value, err := strconv.ParseBool(text)
	if err != nil {
		return false, fmt.Errorf("%w: stream=%q, want true or false", ErrInvalidSetting, text)
	}
	return value, nil
}

func backendKindSetting(cfg contracts.PluginConfig) (BackendKind, error) {
	text := cfg.Get("kind")
	if text == "" {
		return 0, nil
	}
	kind, err := ParseBackendKind(text)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidSetting, err)
	}
	return kind, nil
}
