package cursor

import contracts "github.com/Herrscherd/herrscher-contracts"

// Models is the cursor-agent model catalogue published through Manifest.Models.
// Each ID is also the Arg passed to cursor-agent --model. Effort is part of the
// ID (a suffix such as -high or -thinking-high), so no entry declares efforts.
// Every entry uses the native route, since cursor-agent has no redirection
// lever; a gateway-only host filters the whole catalogue out.
func Models() []contracts.ModelSpec {
	return []contracts.ModelSpec{
		{ID: "auto", Label: "Auto", Arg: "auto", Route: contracts.RouteNative},
		{ID: "claude-opus-5-5-high", Label: "Opus 5.5", Arg: "claude-opus-5-5-high", Route: contracts.RouteNative},
		{ID: "claude-fable-5-1-thinking-high", Label: "Fable 5.1 Thinking", Arg: "claude-fable-5-1-thinking-high", Route: contracts.RouteNative},
		{ID: "claude-sonnet-5-thinking-high", Label: "Sonnet 5 Thinking", Arg: "claude-sonnet-5-thinking-high", Route: contracts.RouteNative},
		{ID: "claude-opus-5-thinking-high", Label: "Opus 5 Thinking", Arg: "claude-opus-5-thinking-high", Route: contracts.RouteNative},
		{ID: "claude-opus-5-high", Label: "Opus 5", Arg: "claude-opus-5-high", Route: contracts.RouteNative},
		{ID: "claude-opus-4-8-high", Label: "Opus 4.8", Arg: "claude-opus-4-8-high", Route: contracts.RouteNative},
		{ID: "claude-fable-5-thinking-high", Label: "Fable 5 Thinking", Arg: "claude-fable-5-thinking-high", Route: contracts.RouteNative},
		{ID: "gpt-5.2", Label: "GPT-5.2", Arg: "gpt-5.2", Route: contracts.RouteNative},
		{ID: "gpt-5.6-sol-high", Label: "GPT-5.6 Sol", Arg: "gpt-5.6-sol-high", Route: contracts.RouteNative},
		{ID: "gpt-5.6-terra-high", Label: "GPT-5.6 Terra", Arg: "gpt-5.6-terra-high", Route: contracts.RouteNative},
		{ID: "gpt-5.6-luna-high", Label: "GPT-5.6 Luna", Arg: "gpt-5.6-luna-high", Route: contracts.RouteNative},
		{ID: "gpt-5.5-high", Label: "GPT-5.5", Arg: "gpt-5.5-high", Route: contracts.RouteNative},
		{ID: "gpt-5.3-codex", Label: "Codex 5.3", Arg: "gpt-5.3-codex", Route: contracts.RouteNative},
		{ID: "gpt-5.3-codex-high", Label: "Codex 5.3 High", Arg: "gpt-5.3-codex-high", Route: contracts.RouteNative},
		{ID: "grok-4.7-high", Label: "Grok 4.7", Arg: "grok-4.7-high", Route: contracts.RouteNative},
		{ID: "cursor-grok-4.5-high", Label: "Grok 4.5", Arg: "cursor-grok-4.5-high", Route: contracts.RouteNative},
		{ID: "gemini-3.7-flash-high", Label: "Gemini 3.7 Flash", Arg: "gemini-3.7-flash-high", Route: contracts.RouteNative},
		{ID: "composer-2.5", Label: "Composer 2.5", Arg: "composer-2.5", Route: contracts.RouteNative},
		{ID: "kimi-k3-high", Label: "Kimi K3", Arg: "kimi-k3-high", Route: contracts.RouteNative},
		{ID: "glm-5.2-high", Label: "GLM 5.2", Arg: "glm-5.2-high", Route: contracts.RouteNative},
	}
}
