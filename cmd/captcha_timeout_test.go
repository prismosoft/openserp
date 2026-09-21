package cmd

import (
	"testing"
	"time"

	"github.com/karust/openserp/core"
)

// The 2captcha client waits 600s for a ReCaptcha result by default, which
// would hold a browser page and its proxy lane for ten minutes — long after
// the caller's own 30s timeout gave up. An unset or nonsense value must land
// on the bounded default, never on the library's.
func TestResolveCaptchaSolveTimeoutBoundsTheWait(t *testing.T) {
	orig := config.Captcha.SolveTimeoutSeconds
	defer func() { config.Captcha.SolveTimeoutSeconds = orig }()

	for _, unset := range []int{0, -1} {
		config.Captcha.SolveTimeoutSeconds = unset
		if got := resolveCaptchaSolveTimeout(); got != core.DefaultCaptchaSolveTimeout {
			t.Fatalf("SolveTimeoutSeconds=%d gave %s, want %s", unset, got, core.DefaultCaptchaSolveTimeout)
		}
	}

	config.Captcha.SolveTimeoutSeconds = 45
	if got := resolveCaptchaSolveTimeout(); got != 45*time.Second {
		t.Fatalf("configured timeout = %s, want 45s", got)
	}

	if core.DefaultCaptchaSolveTimeout >= 600*time.Second {
		t.Fatalf("default %s does not bound the library's 600s wait", core.DefaultCaptchaSolveTimeout)
	}
}

// An enabled solver must hand the browser a bounded timeout, not zero, so a
// misconfiguration cannot silently restore the 600s wait.
func TestResolveCaptchaSolverConfigCarriesABoundedTimeout(t *testing.T) {
	origEnabled := config.Captcha.SolverEnabled
	origKey := config.Config2Capcha.ApiKey
	origTimeout := config.Captcha.SolveTimeoutSeconds
	defer func() {
		config.Captcha.SolverEnabled = origEnabled
		config.Config2Capcha.ApiKey = origKey
		config.Captcha.SolveTimeoutSeconds = origTimeout
	}()

	config.Captcha.SolverEnabled = true
	config.Config2Capcha.ApiKey = "api-key"
	config.Captcha.SolveTimeoutSeconds = 0

	_, _, timeout, err := resolveCaptchaSolverConfig()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if timeout != core.DefaultCaptchaSolveTimeout {
		t.Fatalf("timeout = %s, want %s", timeout, core.DefaultCaptchaSolveTimeout)
	}
}
