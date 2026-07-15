package cursor

import (
	"testing"

	"github.com/Herrscherd/herrscher-contracts"
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
