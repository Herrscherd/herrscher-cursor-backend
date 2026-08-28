package cursor

import (
	"context"

	"github.com/Herrscherd/herrscher-contracts"
)

func init() {
	contracts.Register(contracts.Plugin{
		Manifest: contracts.Manifest{
			Kind:     "cursor",
			Category: contracts.CategoryBackend,
			Status:   contracts.StatusLive,
			Config: []contracts.Setting{
				{Key: "cmd", Env: "CURSOR_CMD", Help: "base command to run Cursor Agent", Default: "cursor-agent"},
				{Key: "model", Env: "CURSOR_MODEL", Help: "model override"},
				{Key: "stream", Env: "CURSOR_STREAM", Help: "resume-based stream mode (false to disable)", Default: "true"},
				{Key: "dir", Env: "CURSOR_DIR", Help: "working directory"},
				{Key: "kind", Env: "CURSOR_KIND", Help: "backend kind"},
			},
			Models: Models,
			// cursor-agent decides on its own what it is allowed to run and offers
			// no hook, no prompt and no approval channel to intercept it. Saying so
			// here is what makes the host warn at create time instead of letting an
			// operator believe a mode they asked for is being honoured.
			Capabilities: contracts.Capabilities{
				Gate:    contracts.GrainNone,
				GateWhy: "cursor-agent exposes no permission hook",
			},
		},
		Backend: func(ctx context.Context, cfg contracts.PluginConfig) (contracts.Backend, error) {
			return NewBackend(ctx, Config{
				Kind:     cfg.Get("kind"),
				Stream:   cfg.Get("stream") != "false",
				Cmd:      cfg.Get("cmd"),
				Model:    cfg.Get("model"),
				Dir:      cfg.Get("dir"),
				ResumeID: cfg.Get("resume"),
			})
		},
	})
}
