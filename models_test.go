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
	for _, m := range Models() {
		if m.Route != contracts.RouteNative {
			t.Errorf("model %q has route %q; cursor cannot be redirected", m.ID, m.Route)
		}
	}
}

func TestModelsDeclareNoSeparateEffort(t *testing.T) {
	for _, m := range Models() {
		if len(m.Efforts) != 0 {
			t.Errorf("model %q declares efforts %v; cursor bakes effort into the ID", m.ID, m.Efforts)
		}
	}
}

func TestGatewayOnlyEliminatesCursor(t *testing.T) {
	if got := contracts.FilterModels(Models(), contracts.PolicyGatewayOnly); len(got) != 0 {
		t.Fatalf("gateway-only policy kept %d cursor models: %+v", len(got), got)
	}
}

// TestManifestPublishesModels pins Manifest.Models on the registered plugin.
// Deleting the "Models: Models" line in register.go was green: the catalog
// tests above all call the Models catalog function directly and never look at
// what the host actually receives from the registry. The consequence is a
// silently empty cursor selector. claude and codex both carry this test.
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
