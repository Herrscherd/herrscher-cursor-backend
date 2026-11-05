package cursor

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestModelsAreValid(t *testing.T) {
	if err := contracts.ValidateModels("cursor", Models()); err != nil {
		t.Fatalf("cursor model catalog is invalid: %v", err)
	}
}

func TestModelsAreNativeOnly(t *testing.T) {
	models := Models()
	for i := range models {
		if models[i].Route != contracts.RouteNative {
			t.Errorf("model %q has route %q; cursor cannot be redirected", models[i].ID, models[i].Route)
		}
	}
}

func TestModelsDeclareNoSeparateEffort(t *testing.T) {
	models := Models()
	for i := range models {
		if len(models[i].Efforts) != 0 {
			t.Errorf("model %q declares efforts %v; cursor bakes effort into the ID", models[i].ID, models[i].Efforts)
		}
	}
}

func TestNativePricingFollowsPublishedTable(t *testing.T) {
	cases := []struct {
		id                           string
		input, cached, write, output float64
		window                       int
	}{
		{"claude-opus-5-5-high", 4, 0.2, 5, 20, 1_000_000},
		{"claude-fable-5-1-thinking-high", 10, 0.25, 12.5, 50, 0},
		{"claude-fable-5-thinking-high", 10, 1, 12.5, 50, 0},
		{"claude-sonnet-5-thinking-high", 2, 0.2, 2.5, 10, 1_000_000},
		{"claude-opus-5-thinking-high", 5, 0.5, 6.25, 25, 1_000_000},
		{"claude-opus-5-high", 5, 0.5, 6.25, 25, 1_000_000},
		{"claude-opus-4-8-high", 5, 0.5, 6.25, 25, 1_000_000},
		{"gpt-5.2", 1.75, 0.175, 0, 14, 0},
		{"gpt-5.5-high", 5, 0.5, 0, 30, 1_000_000},
		{"gpt-5.6-sol-high", 4, 0.4, 5, 20, 1_000_000},
		{"gpt-5.6-terra-high", 2, 0.2, 2.5, 12, 1_000_000},
		{"gpt-5.6-luna-high", 0.2, 0.02, 0.25, 1.2, 1_000_000},
		{"gpt-5.3-codex", 1.75, 0.175, 0, 14, 0},
		{"grok-4.7-high", 2, 0.5, 0, 6, 0},
		{"cursor-grok-4.5-high", 2, 0.5, 0, 6, 0},
		{"gemini-3.7-flash-high", 0.75, 0.075, 0, 3.75, 0},
		{"composer-2.5", 0.5, 0.2, 0, 2.5, 0},
		{"kimi-k3-high", 3, 0.3, 0, 15, 1_000_000},
		{"glm-5.2-high", 1.4, 0.26, 0, 4.4, 0},
	}
	models := Models()
	for _, c := range cases {
		m, ok := findModel(models, c.id)
		if !ok {
			t.Fatalf("model %q missing from catalog", c.id)
		}
		if m.InputPrice != c.input || m.CacheReadPrice != c.cached || m.CacheWritePrice != c.write || m.OutputPrice != c.output || m.ContextWindow != c.window {
			t.Errorf("model %q = in %v cache %v write %v out %v window %d, want in %v cache %v write %v out %v window %d",
				c.id, m.InputPrice, m.CacheReadPrice, m.CacheWritePrice, m.OutputPrice, m.ContextWindow,
				c.input, c.cached, c.write, c.output, c.window)
		}
	}
}

func TestSupersededModelsAreHidden(t *testing.T) {
	models := Models()
	for _, id := range []string{
		"claude-sonnet-5-thinking-high", "claude-opus-5-thinking-high", "claude-opus-5-high",
		"claude-opus-4-8-high", "claude-fable-5-thinking-high", "gpt-5.2", "gpt-5.5-high",
		"gpt-5.3-codex", "gpt-5.3-codex-high", "cursor-grok-4.5-high",
	} {
		m, ok := findModel(models, id)
		if !ok {
			t.Fatalf("model %q missing from catalog", id)
		}
		if !m.Hidden {
			t.Errorf("superseded model %q is visible", id)
		}
	}
}

func TestHiddenByDefaultOnCursorPageStaysHidden(t *testing.T) {
	models := Models()
	for _, id := range []string{"gemini-3.7-flash-high", "glm-5.2-high", "kimi-k3-high"} {
		m, ok := findModel(models, id)
		if !ok {
			t.Fatalf("model %q missing from catalog", id)
		}
		if !m.Hidden {
			t.Errorf("model %q is hidden by default on the cursor page but visible here", id)
		}
	}
}

func TestCurrentModelsStayVisible(t *testing.T) {
	models := Models()
	for _, id := range []string{
		"auto", "claude-opus-5-5-high", "claude-fable-5-1-thinking-high", "gpt-5.6-sol-high",
		"gpt-5.6-terra-high", "gpt-5.6-luna-high", "grok-4.7-high", "composer-2.5",
	} {
		m, ok := findModel(models, id)
		if !ok {
			t.Fatalf("model %q missing from catalog", id)
		}
		if m.Hidden {
			t.Errorf("current model %q is hidden", id)
		}
	}
}

func TestGatewayOnlyEliminatesCursor(t *testing.T) {
	if got := contracts.FilterModels(Models(), contracts.PolicyGatewayOnly); len(got) != 0 {
		t.Fatalf("gateway-only policy kept %d cursor models: %+v", len(got), got)
	}
}

func TestManifestPublishesModels(t *testing.T) {
	var found bool
	for _, p := range contracts.Default.Backends() {
		if p.Manifest.Kind != "cursor" {
			continue
		}
		found = true
		if len(p.Manifest.Models) != len(Models()) {
			t.Fatalf("manifest published %d models, catalog has %d", len(p.Manifest.Models), len(Models()))
		}
	}
	if !found {
		t.Fatal("cursor backend did not self-register")
	}
}

func findModel(models []contracts.ModelSpec, id string) (contracts.ModelSpec, bool) {
	for i := range models {
		if models[i].ID == id {
			return models[i], true
		}
	}
	return contracts.ModelSpec{}, false
}
