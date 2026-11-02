package cursor

import (
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestSelfRegisteredAsBackend(t *testing.T) {
	for _, p := range contracts.Default.Backends() {
		if p.Manifest.Kind == "cursor" {
			if p.Backend == nil {
				t.Fatal("registered cursor plugin has a nil backend factory")
			}
			return
		}
	}
	t.Fatal("cursor backend did not self-register into contracts.Default")
}

func TestDeclaresItCannotGate(t *testing.T) {
	for _, p := range contracts.Default.Backends() {
		if p.Manifest.Kind != "cursor" {
			continue
		}
		if p.Manifest.Capabilities.Gate != contracts.GrainNone {
			t.Fatalf("gate = %q, want none", p.Manifest.Capabilities.Gate)
		}
		if p.Manifest.Capabilities.GateWhy == "" {
			t.Fatal("cursor refuses to gate without telling the operator why")
		}
		return
	}
	t.Fatal("cursor backend did not self-register into contracts.Default")
}
