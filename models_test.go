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

func TestCatalogDeclaresNoContextWindow(t *testing.T) {
	models := Models()
	for i := range models {
		if models[i].ContextWindow != 0 {
			t.Errorf("model %q declares window %d; cursor publishes only up-to limits", models[i].ID, models[i].ContextWindow)
		}
	}
}

func TestNativePricingFollowsPublishedTable(t *testing.T) {
	cases := []struct {
		id                    string
		input, cached, output float64
	}{
		{"claude-opus-5-5-high", 4, 0.2, 20},
		{"claude-fable-5-1-thinking-high", 10, 0.25, 50},
		{"claude-fable-5-thinking-high", 10, 1, 50},
		{"claude-sonnet-5-thinking-high", 2, 0.2, 10},
		{"claude-opus-5-high", 5, 0.5, 25},
		{"claude-opus-4-8-high", 5, 0.5, 25},
		{"gpt-5.2", 1.75, 0.175, 14},
		{"gpt-5.5-high", 5, 0.5, 30},
		{"gpt-5.6-sol-high", 4, 0.4, 20},
		{"gpt-5.6-terra-high", 2, 0.2, 12},
		{"gpt-5.6-luna-high", 0.2, 0.02, 1.2},
		{"gpt-5.3-codex", 1.75, 0.175, 14},
		{"grok-4.7-high", 2, 0.5, 6},
		{"cursor-grok-4.5-high", 2, 0.5, 6},
		{"gemini-3.7-flash-high", 0.75, 0.075, 3.75},
		{"composer-2.5", 0.5, 0.2, 2.5},
		{"kimi-k3-high", 3, 0.3, 15},
		{"glm-5.2-high", 1.4, 0.26, 4.4},
	}
	models := Models()
	for _, c := range cases {
		m, ok := findModel(models, c.id)
		if !ok {
			t.Fatalf("model %q missing from catalog", c.id)
		}
		if m.InputPrice != c.input || m.CacheReadPrice != c.cached || m.OutputPrice != c.output {
			t.Errorf("model %q prices = %v/%v/%v, want %v/%v/%v", c.id, m.InputPrice, m.CacheReadPrice, m.OutputPrice, c.input, c.cached, c.output)
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

func TestCurrentModelsStayVisible(t *testing.T) {
	models := Models()
	for i := range models {
		if models[i].ID == "auto" || models[i].ID == "claude-opus-5-5-high" || models[i].ID == "gpt-5.6-sol-high" || models[i].ID == "grok-4.7-high" {
			if models[i].Hidden {
				t.Errorf("current model %q is hidden", models[i].ID)
			}
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
