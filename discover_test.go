package cursor

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const fakeHangEnv = "HERRSCHER_FAKE_CURSOR_HANG"

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

const listingBody = `printf '%b\n' "Available models" "" "\033[32mauto\033[0m - Auto (current)" "composer-2 - Composer 2 (default)" "gpt-5-mini - GPT-5 mini" "" "Tip: use --model <id> to switch."`

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
	if models[2].ID != "gpt-5-mini" || models[2].Label != "GPT-5 mini" {
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

func TestDiscoverIgnoresStatusAndLabelledNoise(t *testing.T) {
	body := `printf '%s\n' "Loading models..." "Usage:" "Retrying" "Retrying in 2s" "Warning - rate limited" "Error - quota exceeded" "Tip - run cursor-agent login" "Note - see docs" "Models - available" "composer-2 - Composer 2" "gpt-5-mini - GPT-5 mini"`
	models, err := discover(context.Background(), contracts.DiscoveryAccess{Getenv: envWith(fakeCursorAgent(t, body))})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "composer-2" || models[1].ID != "gpt-5-mini" {
		t.Fatalf("models = %+v, want only the two real models", models)
	}
}

func TestDiscoverKeepsStderrOutOfTheListing(t *testing.T) {
	body := `printf '%s\n' "stale-model - Stale" >&2; printf '%s\n' "gpt-5-mini - GPT-5 mini"`
	models, err := discover(context.Background(), contracts.DiscoveryAccess{Getenv: envWith(fakeCursorAgent(t, body))})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "gpt-5-mini" {
		t.Fatalf("models = %+v, want only the stdout model", models)
	}
}

func TestDiscoverAcceptsUppercaseModelIDs(t *testing.T) {
	body := `printf '%s\n' "Claude-Opus-4 - Opus 4" "GPT-5 - GPT 5"`
	models, err := discover(context.Background(), contracts.DiscoveryAccess{Getenv: envWith(fakeCursorAgent(t, body))})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "Claude-Opus-4" || models[1].ID != "GPT-5" {
		t.Fatalf("models = %+v", models)
	}
}

func TestDiscoverOversizedListingIsAnError(t *testing.T) {
	body := `head -c 300000 /dev/zero | tr '\0' 'x'`
	_, err := discover(context.Background(), contracts.DiscoveryAccess{Getenv: envWith(fakeCursorAgent(t, body))})
	if _, ok := errors.AsType[*OutputTooLargeError](err); !ok {
		t.Fatalf("err = %v, want OutputTooLargeError", err)
	}
}

func TestDiscoverEmptyListingListsNothing(t *testing.T) {
	body := `printf '%s\n' "No models available for this account."`
	models, err := discover(context.Background(), contracts.DiscoveryAccess{Getenv: envWith(fakeCursorAgent(t, body))})
	if err != nil || len(models) != 0 {
		t.Fatalf("discover = %v, %v; want empty and no error", models, err)
	}
}

func TestMain(m *testing.M) {
	if os.Getenv(fakeHangEnv) == "1" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeHangingSelf(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeHangEnv, "1")
	return self
}

func TestDiscoverStopsWhenDeadlinePasses(t *testing.T) {
	command := fakeHangingSelf(t)
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

func TestDiscoverGatewayOnlyNeverRunsTheCLI(t *testing.T) {
	access := contracts.DiscoveryAccess{Policy: contracts.PolicyGatewayOnly, Getenv: envWith(filepath.Join(t.TempDir(), "absent"))}
	models, err := discover(context.Background(), access)
	if err != nil || len(models) != 0 {
		t.Fatalf("gateway-only policy ran the CLI: models=%v err=%v", models, err)
	}
}

func TestDiscoverMissingBinaryIsAnError(t *testing.T) {
	_, err := discover(context.Background(), contracts.DiscoveryAccess{Getenv: envWith(filepath.Join(t.TempDir(), "absent"))})
	if err == nil {
		t.Fatal("missing binary listed models")
	}
}

func TestParseModelLineRejectsNoise(t *testing.T) {
	for _, line := range []string{
		"", "Available models", "Tip: use --model <id>", "Error: boom", "two words here", "Loading models...", "Usage:",
		"Retrying", "retrying", "Retrying in 2s", "No models available", "gpt-5-mini",
		"Warning - rate limited", "Error - quota exceeded", "Tip - run cursor-agent login", "Note - see docs", "Models - available",
	} {
		if _, ok := parseModelLine(line); ok {
			t.Fatalf("line %q parsed as a model", line)
		}
	}
}

func TestParseModelLineAcceptsUppercaseWithDigitOrDash(t *testing.T) {
	for _, line := range []string{"Claude-Opus-4 - Opus 4", "GPT-5 - GPT 5", "Gemini-Flash - Gemini Flash", "GPT-Mini - Mini"} {
		if _, ok := parseModelLine(line); !ok {
			t.Fatalf("line %q refused as a model", line)
		}
	}
}

func TestStripANSIRemovesEscapes(t *testing.T) {
	if got := stripANSI("\x1b[1;32mauto\x1b[0m - Auto"); got != "auto - Auto" {
		t.Fatalf("stripANSI = %q", got)
	}
}

func TestKillAfterTreeFailureLogsAndKillsProcess(t *testing.T) {
	logged := captureLog(t)
	cmd := exec.CommandContext(context.Background(), fakeHangingSelf(t), "models")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := killAfterTreeFailure(cmd.Process, errors.New("taskkill refused")); err != nil {
		t.Fatal(err)
	}
	if waitErr := cmd.Wait(); waitErr == nil {
		t.Fatal("process survived its kill")
	}
	if !strings.Contains(logged.String(), "taskkill failed") {
		t.Fatalf("fallback kill not logged: %s", logged)
	}
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestDiscoveredCursorModelsCarryNoPricingOrWindow(t *testing.T) {
	models := parseModelsOutput("claude-opus-5-5-high - Opus 5.5\ngpt-5.6-sol-high - GPT-5.6 Sol\n")
	if len(models) != 2 {
		t.Fatalf("models = %+v", models)
	}
	for i := range models {
		m := models[i]
		if m.InputPrice != 0 || m.OutputPrice != 0 || m.CacheReadPrice != 0 || m.CacheWritePrice != 0 || m.ContextWindow != 0 {
			t.Errorf("discovered model %q carries pricing or window: %+v", m.ID, m)
		}
	}
}
