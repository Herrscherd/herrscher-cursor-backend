package cursor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
)

const (
	statusSignedIn   = `{"status":"authenticated","isAuthenticated":true,"userInfo":{"email":"dev@example.com"},"message":null}`
	statusNoEmail    = `{"status":"authenticated","isAuthenticated":true,"userInfo":{"userId":42}}`
	statusNoDetails  = `{"status":"authenticated","isAuthenticated":true,"message":"Logged in (unable to fetch user details)"}`
	statusLoggedOut  = `{"status":"unauthenticated","isAuthenticated":false,"message":"Not logged in"}`
	statusPartial    = `{"status":"partially-authenticated","isAuthenticated":false}`
	turnSucceeded    = `{"type":"result","subtype":"success","is_error":false,"result":"OK"}`
	turnAuthRequired = `echo "Error: Authentication required. Please run 'agent login' first." >&2; exit 1`
	turnRateLimited  = `{"type":"result","subtype":"error","is_error":true,"result":"You have hit your rate limit."}`
)

func probeConfig(command string) contracts.PluginConfig {
	return contracts.PluginConfig{Settings: map[string]string{"cmd": command}}
}

func fakeCursorFor(t *testing.T, status, turn string) string {
	t.Helper()
	return fakeCursorAgent(t, `if [ "$1" = status ]; then printf '%s\n' '`+status+`'; exit 0; fi
`+turn)
}

func printed(text string) string {
	return `printf '%s\n' '` + text + `'`
}

func runProbe(t *testing.T, access contracts.ProbeAccess) contracts.ProbeReport {
	t.Helper()
	report := probe(t.Context(), access)
	assertRenders(t, report.Detail)
	if report.Remedy.Code != 0 {
		assertRenders(t, report.Remedy)
	}
	return report
}

func assertRenders(t *testing.T, notice contracts.Notice) {
	t.Helper()
	if _, err := contracts.NewRenderer().Render(i18n.English, notice); err != nil {
		t.Fatalf("notice %+v does not render: %v", notice, err)
	}
}

func textArg(notice contracts.Notice, name string) string {
	for _, arg := range notice.Args {
		if arg.Name() == name {
			if text, isText := arg.Value().(string); isText {
				return text
			}
		}
	}
	return ""
}

func TestProbeSignedInReportsTheAccountEmail(t *testing.T) {
	report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorFor(t, statusSignedIn, "exit 9"))})
	if report.State != contracts.HealthOK || report.Detail.Code != contracts.CodeHealthSignedInAs {
		t.Fatalf("report = %+v, want ok signed in", report)
	}
	if name := textArg(report.Detail, "Name"); name != "dev@example.com" {
		t.Fatalf("signed in as %q, want dev@example.com", name)
	}
	if report.Latency <= 0 {
		t.Fatalf("latency = %v, want the measured check", report.Latency)
	}
}

func TestProbeSignedInWithoutEmailIsConnected(t *testing.T) {
	report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorFor(t, statusNoEmail, "exit 9"))})
	if report.State != contracts.HealthOK || report.Detail.Code != contracts.CodeHealthConnected {
		t.Fatalf("report = %+v, want ok connected", report)
	}
}

func TestProbeNotLoggedInIsOffWithTheLoginRemedy(t *testing.T) {
	for _, status := range []string{statusLoggedOut, statusPartial} {
		command := fakeCursorFor(t, status, "exit 9")
		report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(command)})
		if report.State != contracts.HealthOff || report.Detail.Code != contracts.CodeHealthNotLoggedIn {
			t.Errorf("status %s: report = %+v, want off not logged in", status, report)
		}
		if report.Remedy.Code != contracts.CodeHealthRemedyRun || textArg(report.Remedy, "Command") != command+" login" {
			t.Errorf("status %s: remedy = %+v, want %s login", status, report.Remedy, command)
		}
	}
}

func TestProbeRemedyLeavesTheCommandFlagsOut(t *testing.T) {
	command := fakeCursorAgent(t, printed(statusLoggedOut))
	report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(command + " --api-key secret")})
	if remedy := textArg(report.Remedy, "Command"); remedy != command+" login" {
		t.Fatalf("remedy command = %q, want %q", remedy, command+" login")
	}
}

func TestProbeAccountCheckFailureIsDegraded(t *testing.T) {
	report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorFor(t, statusNoDetails, "exit 9"))})
	if report.State != contracts.HealthDegraded || report.Detail.Code != contracts.CodeHealthUnreachable {
		t.Fatalf("report = %+v, want degraded unreachable", report)
	}
	if report.Remedy.Code != contracts.CodeHealthRemedyRun {
		t.Fatalf("remedy = %+v, want the login command", report.Remedy)
	}
}

func TestProbeFailingStatusIsDownWithItsExitStatus(t *testing.T) {
	body := printed(`{"status":"error","message":"Status check error: boom"}`) + "; exit 1"
	report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorAgent(t, body))})
	if report.State != contracts.HealthDown || textArg(report.Detail, "Reason") != "exit status 1" {
		t.Fatalf("report = %+v, want down with exit status 1", report)
	}
}

func TestProbeUnreadableStatusIsDown(t *testing.T) {
	for _, body := range []string{printed("Logged in"), printed(`{"status":"mystery"}`)} {
		report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorAgent(t, body))})
		if report.State != contracts.HealthDown || textArg(report.Detail, "Reason") != reasonUnreadableStatus {
			t.Errorf("body %s: report = %+v, want down unreadable status", body, report)
		}
	}
}

func TestProbeOversizedStatusIsDown(t *testing.T) {
	body := `head -c 300000 /dev/zero | tr '\0' 'x'`
	report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorAgent(t, body))})
	if report.State != contracts.HealthDown || textArg(report.Detail, "Reason") != reasonOutputTooLarge {
		t.Fatalf("report = %+v, want down output too large", report)
	}
}

func TestProbeMissingBinaryIsOff(t *testing.T) {
	report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(filepath.Join(t.TempDir(), "absent"))})
	if report.State != contracts.HealthOff || textArg(report.Detail, "Reason") != reasonCommandNotFound {
		t.Fatalf("report = %+v, want off command not found", report)
	}
}

func TestProbeWithoutCommandOrGetenvLooksUpCursorAgentOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	report := runProbe(t, contracts.ProbeAccess{})
	if report.State != contracts.HealthOff || textArg(report.Detail, "Reason") != reasonCommandNotFound {
		t.Fatalf("report = %+v, want off command not found", report)
	}
}

func TestProbeReadsTheCommandFromTheAccountEnvironment(t *testing.T) {
	report := runProbe(t, contracts.ProbeAccess{Getenv: envWith(fakeCursorFor(t, statusNoEmail, "exit 9"))})
	if report.State != contracts.HealthOK {
		t.Fatalf("report = %+v, want ok through CURSOR_CMD", report)
	}
}

func TestProbeStopsWhenDeadlinePasses(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	report := probe(ctx, contracts.ProbeAccess{Config: probeConfig(fakeHangingSelf(t))})
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("probe took %v past its deadline", elapsed)
	}
	if report.State != contracts.HealthDegraded || textArg(report.Detail, "Reason") != reasonTimeout {
		t.Fatalf("report = %+v, want degraded timeout", report)
	}
}

func TestProbeWithoutDeepRunsNoTurn(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	command := fakeCursorAgent(t, `echo "$@" >> `+calls+`
if [ "$1" = status ]; then printf '%s\n' '`+statusSignedIn+`'; fi`)
	runProbe(t, contracts.ProbeAccess{Config: probeConfig(command)})
	logged, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(logged)); got != "status --format json" {
		t.Fatalf("calls = %q, want the status check only", got)
	}
}

func TestProbeDeepRunsAReadOnlyTurnAndKeepsTheIdentity(t *testing.T) {
	calls := filepath.Join(t.TempDir(), "calls")
	command := fakeCursorFor(t, statusSignedIn, `echo "$@" > `+calls+`; `+printed(turnSucceeded))
	config := probeConfig(command)
	config.Settings["model"] = "composer-2"
	report := runProbe(t, contracts.ProbeAccess{Config: config, Deep: true})
	if report.State != contracts.HealthOK || report.Detail.Code != contracts.CodeHealthSignedInAs {
		t.Fatalf("report = %+v, want ok signed in", report)
	}
	logged, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if turn := string(logged); !strings.Contains(turn, "-p --output-format json --model composer-2 --mode ask") {
		t.Fatalf("turn argv = %q, want a printed ask mode turn on the configured model", turn)
	}
}

func TestProbeDeepRefusedCredentialsAreDown(t *testing.T) {
	report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorFor(t, statusNoEmail, turnAuthRequired)), Deep: true})
	if report.State != contracts.HealthDown || report.Detail.Code != contracts.CodeHealthCredentialsRefused {
		t.Fatalf("report = %+v, want down credentials refused", report)
	}
	if report.Remedy.Code != contracts.CodeHealthRemedyRun {
		t.Fatalf("remedy = %+v, want the login command", report.Remedy)
	}
}

func TestProbeDeepRateLimitIsDegraded(t *testing.T) {
	report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorFor(t, statusNoEmail, printed(turnRateLimited))), Deep: true})
	if report.State != contracts.HealthDegraded || report.Detail.Code != contracts.CodeHealthRateLimited {
		t.Fatalf("report = %+v, want degraded rate limited", report)
	}
}

func TestProbeDeepFailedTurnIsDown(t *testing.T) {
	for _, turn := range []string{printed(`{"type":"result","is_error":true,"result":"boom"}`), printed("not json"), "exit 3"} {
		report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorFor(t, statusNoEmail, turn)), Deep: true})
		if report.State != contracts.HealthDown || report.Detail.Code != contracts.CodeHealthUnreachable {
			t.Errorf("turn %s: report = %+v, want down unreachable", turn, report)
		}
	}
}

func TestProbeDeepTurnSettlesAFailedAccountCheck(t *testing.T) {
	refused := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorFor(t, statusNoDetails, turnAuthRequired)), Deep: true})
	if refused.State != contracts.HealthDown || refused.Detail.Code != contracts.CodeHealthCredentialsRefused {
		t.Errorf("refused turn: report = %+v, want down credentials refused", refused)
	}
	answered := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorFor(t, statusNoDetails, printed(turnSucceeded))), Deep: true})
	if answered.State != contracts.HealthOK || answered.Detail.Code != contracts.CodeHealthConnected || answered.Remedy.Code != 0 {
		t.Errorf("answered turn: report = %+v, want ok connected without remedy", answered)
	}
}

func TestProbeDeepSkipsTheTurnWhenNotLoggedIn(t *testing.T) {
	report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorFor(t, statusLoggedOut, printed(turnSucceeded))), Deep: true})
	if report.State != contracts.HealthOff {
		t.Fatalf("report = %+v, want off without a turn", report)
	}
}

func TestRegisteredPluginProbesAndReadsNoLimits(t *testing.T) {
	plugin := cursorPlugin(t)
	if plugin.Limits != nil {
		t.Fatal("cursor declares a limit reader, but cursor-agent exposes no limits without a turn")
	}
	if plugin.Probe == nil {
		t.Fatal("cursor registered no probe")
	}
	report := plugin.Probe(t.Context(), contracts.ProbeAccess{Config: probeConfig(fakeCursorFor(t, statusLoggedOut, "exit 9"))})
	if report.State != contracts.HealthOff {
		t.Fatalf("report = %+v, want off", report)
	}
}

func TestProbeDeepWithMissingDirIsDown(t *testing.T) {
	config := probeConfig(fakeCursorFor(t, statusSignedIn, printed(turnSucceeded)))
	config.Settings["dir"] = filepath.Join(t.TempDir(), "absent")
	report := runProbe(t, contracts.ProbeAccess{Config: config, Deep: true})
	if report.State != contracts.HealthDown || textArg(report.Detail, "Reason") != reasonDirMissing {
		t.Fatalf("report = %+v, want down with %q", report, reasonDirMissing)
	}
}

func TestProbeRefusesAProfile(t *testing.T) {
	report := runProbe(t, contracts.ProbeAccess{Config: probeConfig(fakeCursorFor(t, statusSignedIn, "")), Account: "work"})
	if report.State != contracts.HealthOff || textArg(report.Detail, "Reason") != reasonProfileUnsupported {
		t.Fatalf("report = %+v, want off with %q", report, reasonProfileUnsupported)
	}
}
