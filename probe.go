package cursor

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const (
	loginStatusAuthenticated   = "authenticated"
	loginStatusPartial         = "partially-authenticated"
	loginStatusUnauthenticated = "unauthenticated"
	deepProbePrompt            = "Reply with the single word OK."
	rateLimitText              = "rate limit"
	reasonTimeout              = "timeout"
	reasonCanceled             = "canceled"
	reasonCommandNotFound      = "command not found"
	reasonCommandFailed        = "command failed"
	reasonOutputTooLarge       = "output too large"
	reasonUnreadableStatus     = "unreadable status"
	reasonAccountCheckFailed   = "account check failed"
	reasonTurnFailed           = "turn failed"
	reasonDirMissing           = "working directory missing"
	reasonProfileUnsupported   = "profiles not supported"
)

type loginStatus struct {
	Status   string `json:"status"`
	UserInfo *struct {
		Email string `json:"email"`
	} `json:"userInfo"`
}

func probe(ctx context.Context, access contracts.ProbeAccess) contracts.ProbeReport {
	if access.Account != "" {
		return unreachableReport(contracts.HealthOff, reasonProfileUnsupported)
	}
	base := probeCommand(access)
	started := time.Now()
	report, isSignedIn := statusReport(ctx, base)
	if access.Deep && isSignedIn {
		report = deepReport(ctx, base, access.Config, report)
	}
	report.Latency = time.Since(started)
	return report
}

func probeCommand(access contracts.ProbeAccess) []string {
	if configured := access.Config.Get("cmd"); configured != "" {
		return baseCommand(configured)
	}
	if access.Getenv == nil {
		return baseCommand("")
	}
	return baseCommand(access.Getenv("CURSOR_CMD"))
}

func statusReport(ctx context.Context, base []string) (contracts.ProbeReport, bool) {
	output, err := runBounded(ctx, append(slices.Clone(base), "status", "--format", "json"), "")
	if err != nil {
		return failureReport(ctx, err), false
	}
	if output.stdout.exceeded {
		return unreachableReport(contracts.HealthDown, reasonOutputTooLarge), false
	}
	var status loginStatus
	if json.Unmarshal(output.stdout.data.Bytes(), &status) != nil {
		return unreachableReport(contracts.HealthDown, reasonUnreadableStatus), false
	}
	return loginReport(status, base)
}

func loginReport(status loginStatus, base []string) (contracts.ProbeReport, bool) {
	switch status.Status {
	case loginStatusAuthenticated:
		return signedInReport(status, base), true
	case loginStatusPartial, loginStatusUnauthenticated:
		return contracts.ProbeReport{
			State:  contracts.HealthOff,
			Detail: contracts.Notice{Code: contracts.CodeHealthNotLoggedIn},
			Remedy: loginRemedy(base),
		}, false
	}
	return unreachableReport(contracts.HealthDown, reasonUnreadableStatus), false
}

func signedInReport(status loginStatus, base []string) contracts.ProbeReport {
	switch {
	case status.UserInfo == nil:
		report := unreachableReport(contracts.HealthDegraded, reasonAccountCheckFailed)
		report.Remedy = loginRemedy(base)
		return report
	case status.UserInfo.Email != "":
		return contracts.ProbeReport{
			State:  contracts.HealthOK,
			Detail: contracts.Notice{Code: contracts.CodeHealthSignedInAs, Args: []contracts.Arg{contracts.Text("Name", status.UserInfo.Email)}},
		}
	}
	return connectedReport()
}

func connectedReport() contracts.ProbeReport {
	return contracts.ProbeReport{State: contracts.HealthOK, Detail: contracts.Notice{Code: contracts.CodeHealthConnected}}
}

func deepReport(ctx context.Context, base []string, config contracts.PluginConfig, signedIn contracts.ProbeReport) contracts.ProbeReport {
	if failed, isFailed := deepFailure(ctx, base, config); isFailed {
		return failed
	}
	if signedIn.State != contracts.HealthOK {
		return connectedReport()
	}
	return signedIn
}

func deepFailure(ctx context.Context, base []string, config contracts.PluginConfig) (contracts.ProbeReport, bool) {
	dir := config.Get("dir")
	if dir != "" {
		if _, err := os.Stat(dir); err != nil {
			return unreachableReport(contracts.HealthDown, reasonDirMissing), true
		}
	}
	argv := append(baseArgv(base, "json", config.Get("model"), ""), "--mode", "ask", deepProbePrompt)
	output, err := runBounded(ctx, argv, dir)
	if err == nil && isAnsweredTurn(output) {
		return contracts.ProbeReport{}, false
	}
	switch {
	case output.mentions(authRequiredText):
		return contracts.ProbeReport{
			State:  contracts.HealthDown,
			Detail: contracts.Notice{Code: contracts.CodeHealthCredentialsRefused},
			Remedy: loginRemedy(base),
		}, true
	case output.mentions(rateLimitText):
		return contracts.ProbeReport{State: contracts.HealthDegraded, Detail: contracts.Notice{Code: contracts.CodeHealthRateLimited}}, true
	case err != nil:
		return failureReport(ctx, err), true
	case output.stdout.exceeded:
		return unreachableReport(contracts.HealthDown, reasonOutputTooLarge), true
	}
	return unreachableReport(contracts.HealthDown, reasonTurnFailed), true
}

func isAnsweredTurn(output *boundedOutput) bool {
	var event cursorEvent
	if output.stdout.exceeded || json.Unmarshal(output.stdout.data.Bytes(), &event) != nil {
		return false
	}
	return event.Type == cursorEventResult && !event.IsError
}

func failureReport(ctx context.Context, err error) contracts.ProbeReport {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return unreachableReport(contracts.HealthDegraded, reasonTimeout)
	case ctx.Err() != nil:
		return unreachableReport(contracts.HealthDegraded, reasonCanceled)
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return unreachableReport(contracts.HealthOff, reasonCommandNotFound)
	}
	if exited, isExit := errors.AsType[*exec.ExitError](err); isExit {
		return unreachableReport(contracts.HealthDown, exited.String())
	}
	return unreachableReport(contracts.HealthDown, reasonCommandFailed)
}

func unreachableReport(state contracts.HealthState, reason string) contracts.ProbeReport {
	return contracts.ProbeReport{
		State:  state,
		Detail: contracts.Notice{Code: contracts.CodeHealthUnreachable, Args: []contracts.Arg{contracts.Text("Reason", reason)}},
	}
}

func loginRemedy(base []string) contracts.Notice {
	return contracts.Notice{Code: contracts.CodeHealthRemedyRun, Args: []contracts.Arg{contracts.Text("Command", base[0]+" login")}}
}
