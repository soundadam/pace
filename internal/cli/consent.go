package cli

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"github.com/soundadam/soundprobe/internal/consent"
)

// M-Lab publishes measurement data, so it is the one provider that needs a
// recorded, interactive consent before it runs.  Consent is never granted
// implicitly: a non-interactive invocation fails closed instead.

func (app *App) executeConsentStatus(jsonMode bool) int {
	if app.Consent == nil {
		return app.fail(jsonMode, "consent_error", "consent store is not configured", 1)
	}
	record, accepted, err := app.Consent.Status()
	if err != nil {
		return app.fail(jsonMode, "consent_error", err.Error(), 1)
	}
	if jsonMode {
		payload := map[string]any{
			"accepted":      accepted,
			"policyVersion": consent.PolicyVersion,
			"policyUrl":     consent.PolicyURL,
		}
		if !record.AcceptedAt.IsZero() {
			payload["record"] = record
		}
		return app.writeValue(true, payload, "")
	}
	out, styles := app.humanOutput()
	if accepted {
		fmt.Fprintln(out, styles.OK(fmt.Sprintf("M-Lab consent accepted (%s at %s).", record.PolicyVersion, record.AcceptedAt.Format(time.RFC3339))))
	} else {
		fmt.Fprintln(out, styles.Warn(fmt.Sprintf("M-Lab consent is not accepted for current policy %s.", consent.PolicyVersion)))
	}
	fmt.Fprintf(out, "Policy: %s\n", styles.Accent(consent.PolicyURL))
	return 0
}

func (app *App) executeConsentAccept(jsonMode bool) int {
	if app.Consent == nil {
		return app.fail(jsonMode, "consent_error", "consent store is not configured", 1)
	}
	if jsonMode {
		return app.fail(true, "consent_requires_interaction", "consent accept is interactive and unavailable in JSON mode", 1)
	}
	return app.promptAndAcceptConsent(false)
}

func (app *App) executeConsentRevoke(jsonMode bool) int {
	if app.Consent == nil {
		return app.fail(jsonMode, "consent_error", "consent store is not configured", 1)
	}
	if err := app.Consent.Revoke(); err != nil {
		return app.fail(jsonMode, "consent_error", err.Error(), 1)
	}
	return app.writeValue(jsonMode, map[string]bool{"revoked": true}, "M-Lab consent revoked.")
}

func (app *App) ensureMLabConsent(jsonMode bool) int {
	if app.Consent == nil {
		return app.fail(jsonMode, "consent_error", "consent store is not configured", 1)
	}
	_, accepted, err := app.Consent.Status()
	if err != nil {
		return app.fail(jsonMode, "consent_error", err.Error(), 1)
	}
	if accepted {
		return 0
	}
	if jsonMode || !app.StdinTTY {
		return app.fail(jsonMode, "consent_required", "M-Lab consent is required; run `soundprobe consent accept` interactively", 1)
	}
	return app.promptAndAcceptConsent(jsonMode)
}

func (app *App) promptAndAcceptConsent(jsonMode bool) int {
	if !app.StdinTTY {
		return app.fail(jsonMode, "consent_requires_interaction", "consent acceptance requires an interactive terminal", 1)
	}
	out, styles := app.humanOutput()
	fmt.Fprintln(out, styles.Title("M-Lab measurement consent"))
	fmt.Fprintln(out, "M-Lab collects the ISP-provided public IP address and measurement results.")
	fmt.Fprintln(out, "M-Lab publishes and retains experiment data indefinitely.")
	fmt.Fprintf(out, "Policy %s: %s\n", consent.PolicyVersion, styles.Accent(consent.PolicyURL))
	fmt.Fprint(out, styles.Title("Type accept to continue: "))
	scanner := bufio.NewScanner(app.In)
	if !scanner.Scan() {
		return app.fail(jsonMode, "consent_declined", "consent was not accepted", 1)
	}
	if strings.TrimSpace(scanner.Text()) != "accept" {
		return app.fail(jsonMode, "consent_declined", "consent was not accepted", 1)
	}
	record, err := app.Consent.Accept(app.Version, app.Now())
	if err != nil {
		return app.fail(jsonMode, "consent_error", err.Error(), 1)
	}
	fmt.Fprintln(out, styles.OK(fmt.Sprintf("M-Lab consent recorded for policy %s.", record.PolicyVersion)))
	return 0
}
