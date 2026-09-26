package cursor

import "github.com/Herrscherd/herrscher-contracts"

// Models is the cursor-agent model catalog. The IDs are those verified live
// via `cursor-agent models` at commit 3557344 — the previous iteration listed
// nonexistent models, which produced "Cannot use this model" at startup.
//
// Effort is encoded IN the ID (suffix -high, -thinking-…): no separate axis,
// so Efforts is empty everywhere.
//
// All entries are on native route: cursor-agent exposes no redirection lever.
// The public build, which accepts only gateway route, eliminates them all
// without special handling.
var Models = []contracts.ModelSpec{
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
