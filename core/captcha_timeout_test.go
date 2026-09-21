package core

import (
	"testing"
	"time"

	api2captcha "github.com/2captcha/2captcha-go"
)

// NewSolver must always configure a bounded wait. api2captcha would poll for
// 600s, pinning a browser page and its proxy lane for ten minutes — long after
// the caller's own timeout has given up.
func TestNewSolverBoundsTheSolveWait(t *testing.T) {
	cases := map[string]struct {
		given time.Duration
		want  time.Duration
	}{
		"explicit": {given: 45 * time.Second, want: 45 * time.Second},
		"zero":     {given: 0, want: DefaultCaptchaSolveTimeout},
		"negative": {given: -1 * time.Second, want: DefaultCaptchaSolveTimeout},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			client, ok := NewSolver("api-key", tc.given).client.(*twoCaptchaAPIClient)
			if !ok {
				t.Fatalf("solver client is %T", NewSolver("api-key", tc.given).client)
			}
			if client.timeout != tc.want {
				t.Fatalf("timeout = %s, want %s", client.timeout, tc.want)
			}
		})
	}
}

// The library's own default is the hazard this bounds; if a dependency bump
// ever made it shorter than ours, ours would be the pessimisation.
func TestDefaultCaptchaSolveTimeoutIsShorterThanTheLibrarys(t *testing.T) {
	libraryDefault := api2captcha.NewClient("api-key").RecaptchaTimeout
	if int(DefaultCaptchaSolveTimeout/time.Second) >= libraryDefault {
		t.Fatalf("default %s does not bound the library's %ds wait",
			DefaultCaptchaSolveTimeout, libraryDefault)
	}
}
