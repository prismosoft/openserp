package core

import (
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	api2captcha "github.com/2captcha/2captcha-go"
)

type captchaClient interface {
	// Solve submits the captcha and waits for its answer. transportProxyURL
	// routes the API call itself, so the request reaches 2captcha from the
	// same lane the solve is being bought for rather than from the
	// deployment's datacenter IP.
	Solve(req api2captcha.Request, transportProxyURL string) (string, string, error)
}

type CaptchaSolver struct {
	client captchaClient
}

// DefaultCaptchaSolveTimeout bounds how long a single solve may be waited on.
//
// The 2captcha client defaults to 600s for ReCaptcha, polling every 10s. That
// wait blocks a browser page and its proxy lane for ten minutes — long after
// the caller's own timeout (30s for VidBlitz) has given up on the request. The
// solve is still worth starting, because clearing the challenge warms the lane
// for the requests behind it, but it must not hold resources indefinitely to
// do so.
const DefaultCaptchaSolveTimeout = 100 * time.Second

// NewSolver builds a 2captcha-backed solver. A non-positive solveTimeout falls
// back to DefaultCaptchaSolveTimeout.
func NewSolver(apikey string, solveTimeout time.Duration) *CaptchaSolver {
	if solveTimeout <= 0 {
		solveTimeout = DefaultCaptchaSolveTimeout
	}
	return &CaptchaSolver{
		client: newTwoCaptchaAPIClient(apikey, solveTimeout),
	}
}

var (
	captchaSolverAttemptsTotal  atomic.Uint64
	captchaSolverSuccessesTotal atomic.Uint64
	captchaSolverFailuresTotal  atomic.Uint64
)

func (cs *CaptchaSolver) SolveReCaptcha2(sitekey, pageURL, dataS, proxyURL string) (string, string, error) {
	captchaSolverAttemptsTotal.Add(1)

	cap := api2captcha.ReCaptcha{
		SiteKey:   sitekey,
		Url:       pageURL,
		DataS:     dataS,
		Invisible: false,
		Action:    "verify",
	}
	req := cap.ToRequest()

	if proxyType, proxyAddr, ok := toCaptchaProxy(proxyURL); ok {
		req.SetProxy(proxyType, proxyAddr)
	}

	resp, id, err := cs.client.Solve(req, proxyURL)
	if err != nil {
		captchaSolverFailuresTotal.Add(1)
		return resp, id, err
	}

	captchaSolverSuccessesTotal.Add(1)
	return resp, id, nil
}

func toCaptchaProxy(raw string) (string, string, bool) {
	normalized, err := NormalizeProxyURL(raw)
	if err != nil || normalized == "" {
		return "", "", false
	}

	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Host == "" {
		return "", "", false
	}

	var proxyType string
	switch strings.ToLower(parsed.Scheme) {
	case "http":
		proxyType = "HTTP"
	case "https":
		proxyType = "HTTPS"
	case "socks5", "socks5h":
		proxyType = "SOCKS5"
	default:
		return "", "", false
	}

	proxyAddr := parsed.Host
	if parsed.User != nil {
		user := parsed.User.Username()
		password, _ := parsed.User.Password()
		if user != "" && password != "" {
			proxyAddr = user + ":" + password + "@" + parsed.Host
		} else if user != "" {
			proxyAddr = user + "@" + parsed.Host
		}
	}

	return proxyType, proxyAddr, true
}

func CaptchaSolverMetrics() map[string]uint64 {
	return map[string]uint64{
		"solver_attempts":  captchaSolverAttemptsTotal.Load(),
		"solver_successes": captchaSolverSuccessesTotal.Load(),
		"solver_failures":  captchaSolverFailuresTotal.Load(),
	}
}

func resetCaptchaSolverMetrics() {
	captchaSolverAttemptsTotal.Store(0)
	captchaSolverSuccessesTotal.Store(0)
	captchaSolverFailuresTotal.Store(0)
}
