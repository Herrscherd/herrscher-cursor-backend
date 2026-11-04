package cursor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func fakeCursorAgent(t *testing.T, body string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "cursor-agent")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return script
}

func envWith(command string) func(string) string {
	return func(name string) string {
		if name == "CURSOR_CMD" {
			return command
		}
		return ""
	}
}

const listingBody = `printf '%b\n' "Available models" "" "\033[32mauto\033[0m - Auto (current)" "composer-2 - Composer 2 (default)" "gpt-5-mini" "" "Tip: use --model <id> to switch."`

func TestDiscoverParsesListing(t *testing.T) {
	models, err := discover(context.Background(), contracts.DiscoveryAccess{Getenv: envWith(fakeCursorAgent(t, listingBody))})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 3 {
		t.Fatalf("models = %+v", models)
	}
	if models[0].ID != "auto" || models[0].Label != "Auto" || models[0].Arg != "auto" || models[0].Route != contracts.RouteNative {
		t.Fatalf("first model = %+v", models[0])
	}
	if models[1].ID != "composer-2" || models[1].Label != "Composer 2" {
		t.Fatalf("second model = %+v", models[1])
	}
	if models[2].ID != "gpt-5-mini" || models[2].Label != "" {
		t.Fatalf("third model = %+v", models[2])
	}
}

func TestDiscoverAuthenticationRequiredIsSilent(t *testing.T) {
	body := `printf '%s\n' "Error: Authentication required. Run 'agent login' to authenticate." >&2; exit 1`
	models, err := discover(context.Background(), contracts.DiscoveryAccess{Getenv: envWith(fakeCursorAgent(t, body))})
	if err != nil || models != nil {
		t.Fatalf("discover = %v, %v; want nil, nil", models, err)
	}
}

func TestDiscoverOtherFailureIsAnError(t *testing.T) {
	body := `printf '%s\n' "network down" >&2; exit 3`
	_, err := discover(context.Background(), contracts.DiscoveryAccess{Getenv: envWith(fakeCursorAgent(t, body))})
	if err == nil {
		t.Fatal("a failing listing returned no error")
	}
}

func TestDiscoverEmptyListingListsNothing(t *testing.T) {
	body := `printf '%s\n' "No models available for this account."`
	models, err := discover(context.Background(), contracts.DiscoveryAccess{Getenv: envWith(fakeCursorAgent(t, body))})
	if err != nil || len(models) != 0 {
		t.Fatalf("discover = %v, %v; want empty and no error", models, err)
	}
}

func TestDiscoverStopsWhenDeadlinePasses(t *testing.T) {
	command := fakeCursorAgent(t, "sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := discover(ctx, contracts.DiscoveryAccess{Getenv: envWith(command)})
	if err == nil {
		t.Fatal("a hanging listing returned no error")
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("discover took %v past its deadline", elapsed)
	}
}

func TestDiscoverMissingBinaryIsAnError(t *testing.T) {
	_, err := discover(context.Background(), contracts.DiscoveryAccess{Getenv: envWith(filepath.Join(t.TempDir(), "absent"))})
	if err == nil {
		t.Fatal("missing binary listed models")
	}
}

func TestParseModelLineRejectsNoise(t *testing.T) {
	for _, line := range []string{"", "Available models", "Tip: use --model <id>", "Error: boom", "two words here"} {
		if _, ok := parseModelLine(line); ok {
			t.Fatalf("line %q parsed as a model", line)
		}
	}
}

func TestStripANSIRemovesEscapes(t *testing.T) {
	if got := stripANSI("\x1b[1;32mauto\x1b[0m - Auto"); got != "auto - Auto" {
		t.Fatalf("stripANSI = %q", got)
	}
}
