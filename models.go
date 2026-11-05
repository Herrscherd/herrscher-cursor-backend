package cursor

import contracts "github.com/Herrscherd/herrscher-contracts"

const (
	opus55In, opus55Cached, opus55Out, opus55Write                 = 4, 0.2, 20, 5
	fable51In, fable51Cached, fable51Out, fable51Write             = 10, 0.25, 50, 12.5
	fable5In, fable5Cached, fable5Out, fable5Write                 = 10, 1, 50, 12.5
	sonnet5In, sonnet5Cached, sonnet5Out, sonnet5Write             = 2, 0.2, 10, 2.5
	opus5In, opus5Cached, opus5Out, opus5Write                     = 5, 0.5, 25, 6.25
	gpt52In, gpt52Cached, gpt52Out                                 = 1.75, 0.175, 14
	gpt55In, gpt55Cached, gpt55Out                                 = 5, 0.5, 30
	gpt56SolIn, gpt56SolCached, gpt56SolOut, gpt56SolWrite         = 4, 0.4, 20, 5
	gpt56TerraIn, gpt56TerraCached, gpt56TerraOut, gpt56TerraWrite = 2, 0.2, 12, 2.5
	gpt56LunaIn, gpt56LunaCached, gpt56LunaOut, gpt56LunaWrite     = 0.2, 0.02, 1.2, 0.25
	gpt53CodexIn, gpt53CodexCached, gpt53CodexOut                  = 1.75, 0.175, 14
	grok47In, grok47Cached, grok47Out                              = 2, 0.5, 6
	grok45In, grok45Cached, grok45Out                              = 2, 0.5, 6
	gemini37FlashIn, gemini37FlashCached, gemini37FlashOut         = 0.75, 0.075, 3.75
	composer25In, composer25Cached, composer25Out                  = 0.5, 0.2, 2.5
	kimiK3In, kimiK3Cached, kimiK3Out                              = 3, 0.3, 15
	glm52In, glm52Cached, glm52Out                                 = 1.4, 0.26, 4.4
)

const context1M = 1_000_000

// Models returns the cursor-agent catalogue, native route only.
func Models() []contracts.ModelSpec {
	return []contracts.ModelSpec{
		{ID: "auto", Label: "Auto", Arg: "auto", Route: contracts.RouteNative},
		{ID: "claude-opus-5-5-high", Label: "Opus 5.5", Arg: "claude-opus-5-5-high", Route: contracts.RouteNative, InputPrice: opus55In, OutputPrice: opus55Out, CacheReadPrice: opus55Cached, CacheWritePrice: opus55Write, ContextWindow: context1M},
		{ID: "claude-fable-5-1-thinking-high", Label: "Fable 5.1 Thinking", Arg: "claude-fable-5-1-thinking-high", Route: contracts.RouteNative, InputPrice: fable51In, OutputPrice: fable51Out, CacheReadPrice: fable51Cached, CacheWritePrice: fable51Write},
		{ID: "claude-sonnet-5-thinking-high", Label: "Sonnet 5 Thinking", Arg: "claude-sonnet-5-thinking-high", Route: contracts.RouteNative, InputPrice: sonnet5In, OutputPrice: sonnet5Out, CacheReadPrice: sonnet5Cached, CacheWritePrice: sonnet5Write, ContextWindow: context1M, Hidden: true},
		{ID: "claude-opus-5-thinking-high", Label: "Opus 5 Thinking", Arg: "claude-opus-5-thinking-high", Route: contracts.RouteNative, InputPrice: opus5In, OutputPrice: opus5Out, CacheReadPrice: opus5Cached, CacheWritePrice: opus5Write, ContextWindow: context1M, Hidden: true},
		{ID: "claude-opus-5-high", Label: "Opus 5", Arg: "claude-opus-5-high", Route: contracts.RouteNative, InputPrice: opus5In, OutputPrice: opus5Out, CacheReadPrice: opus5Cached, CacheWritePrice: opus5Write, ContextWindow: context1M, Hidden: true},
		{ID: "claude-opus-4-8-high", Label: "Opus 4.8", Arg: "claude-opus-4-8-high", Route: contracts.RouteNative, InputPrice: opus5In, OutputPrice: opus5Out, CacheReadPrice: opus5Cached, CacheWritePrice: opus5Write, ContextWindow: context1M, Hidden: true},
		{ID: "claude-fable-5-thinking-high", Label: "Fable 5 Thinking", Arg: "claude-fable-5-thinking-high", Route: contracts.RouteNative, InputPrice: fable5In, OutputPrice: fable5Out, CacheReadPrice: fable5Cached, CacheWritePrice: fable5Write, Hidden: true},
		{ID: "gpt-5.2", Label: "GPT-5.2", Arg: "gpt-5.2", Route: contracts.RouteNative, InputPrice: gpt52In, OutputPrice: gpt52Out, CacheReadPrice: gpt52Cached, Hidden: true},
		{ID: "gpt-5.6-sol-high", Label: "GPT-5.6 Sol", Arg: "gpt-5.6-sol-high", Route: contracts.RouteNative, InputPrice: gpt56SolIn, OutputPrice: gpt56SolOut, CacheReadPrice: gpt56SolCached, CacheWritePrice: gpt56SolWrite, ContextWindow: context1M},
		{ID: "gpt-5.6-terra-high", Label: "GPT-5.6 Terra", Arg: "gpt-5.6-terra-high", Route: contracts.RouteNative, InputPrice: gpt56TerraIn, OutputPrice: gpt56TerraOut, CacheReadPrice: gpt56TerraCached, CacheWritePrice: gpt56TerraWrite, ContextWindow: context1M},
		{ID: "gpt-5.6-luna-high", Label: "GPT-5.6 Luna", Arg: "gpt-5.6-luna-high", Route: contracts.RouteNative, InputPrice: gpt56LunaIn, OutputPrice: gpt56LunaOut, CacheReadPrice: gpt56LunaCached, CacheWritePrice: gpt56LunaWrite, ContextWindow: context1M},
		{ID: "gpt-5.5-high", Label: "GPT-5.5", Arg: "gpt-5.5-high", Route: contracts.RouteNative, InputPrice: gpt55In, OutputPrice: gpt55Out, CacheReadPrice: gpt55Cached, ContextWindow: context1M, Hidden: true},
		{ID: "gpt-5.3-codex", Label: "Codex 5.3", Arg: "gpt-5.3-codex", Route: contracts.RouteNative, InputPrice: gpt53CodexIn, OutputPrice: gpt53CodexOut, CacheReadPrice: gpt53CodexCached, Hidden: true},
		{ID: "gpt-5.3-codex-high", Label: "Codex 5.3 High", Arg: "gpt-5.3-codex-high", Route: contracts.RouteNative, InputPrice: gpt53CodexIn, OutputPrice: gpt53CodexOut, CacheReadPrice: gpt53CodexCached, Hidden: true},
		{ID: "grok-4.7-high", Label: "Grok 4.7", Arg: "grok-4.7-high", Route: contracts.RouteNative, InputPrice: grok47In, OutputPrice: grok47Out, CacheReadPrice: grok47Cached},
		{ID: "cursor-grok-4.5-high", Label: "Grok 4.5", Arg: "cursor-grok-4.5-high", Route: contracts.RouteNative, InputPrice: grok45In, OutputPrice: grok45Out, CacheReadPrice: grok45Cached, Hidden: true},
		{ID: "gemini-3.7-flash-high", Label: "Gemini 3.7 Flash", Arg: "gemini-3.7-flash-high", Route: contracts.RouteNative, InputPrice: gemini37FlashIn, OutputPrice: gemini37FlashOut, CacheReadPrice: gemini37FlashCached, Hidden: true},
		{ID: "composer-2.5", Label: "Composer 2.5", Arg: "composer-2.5", Route: contracts.RouteNative, InputPrice: composer25In, OutputPrice: composer25Out, CacheReadPrice: composer25Cached},
		{ID: "kimi-k3-high", Label: "Kimi K3", Arg: "kimi-k3-high", Route: contracts.RouteNative, InputPrice: kimiK3In, OutputPrice: kimiK3Out, CacheReadPrice: kimiK3Cached, ContextWindow: context1M, Hidden: true},
		{ID: "glm-5.2-high", Label: "GLM 5.2", Arg: "glm-5.2-high", Route: contracts.RouteNative, InputPrice: glm52In, OutputPrice: glm52Out, CacheReadPrice: glm52Cached, Hidden: true},
	}
}
