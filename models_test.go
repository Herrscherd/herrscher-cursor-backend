package cursor

import (
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
)

func TestModelsAreValid(t *testing.T) {
	if err := contracts.ValidateModels("cursor", Models); err != nil {
		t.Fatalf("cursor model catalog is invalid: %v", err)
	}
}

func TestModelsAreNativeOnly(t *testing.T) {
	// cursor-agent exposes no redirection lever. A gateway model here would be
	// impossible to serve — and this property is what excludes cursor from the
	// public build without explicit removal.
	for _, m := range Models {
		if m.Route != contracts.RouteNative {
			t.Errorf("model %q has route %q; cursor cannot be redirected", m.ID, m.Route)
		}
	}
}

func TestModelsDeclareNoSeparateEffort(t *testing.T) {
	// Cursor effort is encoded in the model ID (suffix -high). Declaring a
	// separate axis would produce a --effort that the CLI rejects.
	for _, m := range Models {
		if len(m.Efforts) != 0 {
			t.Errorf("model %q declares efforts %v; cursor bakes effort into the ID", m.ID, m.Efforts)
		}
	}
}

func TestGatewayOnlyEliminatesCursor(t *testing.T) {
	if got := contracts.FilterModels(Models, contracts.PolicyGatewayOnly); len(got) != 0 {
		t.Fatalf("gateway-only policy kept %d cursor models: %+v", len(got), got)
	}
}
