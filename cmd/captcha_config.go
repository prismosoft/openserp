package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/karust/openserp/core"
	"github.com/sirupsen/logrus"
)

func resolveCaptchaSolverConfig() (bool, string, time.Duration, error) {
	apiKey := strings.TrimSpace(config.Config2Capcha.ApiKey)
	if !config.Captcha.SolverEnabled {
		if apiKey != "" {
			logrus.Warn("2captcha.apikey is set but captcha.solver_enabled=false; solver will not run")
		}
		return false, "", 0, nil
	}

	if apiKey == "" {
		return false, "", 0, fmt.Errorf("captcha solver is enabled (captcha.solver_enabled=true) but 2captcha.apikey is empty")
	}

	return true, apiKey, resolveCaptchaSolveTimeout(), nil
}

// resolveCaptchaSolveTimeout keeps a misconfigured or unset value from falling
// back to the 2captcha client's 600s wait, which would pin a page and its
// proxy lane for ten minutes.
func resolveCaptchaSolveTimeout() time.Duration {
	seconds := config.Captcha.SolveTimeoutSeconds
	if seconds <= 0 {
		return core.DefaultCaptchaSolveTimeout
	}
	return time.Duration(seconds) * time.Second
}
