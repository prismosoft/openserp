package core

import (
	"testing"
	"time"

	api2captcha "github.com/2captcha/2captcha-go"
)

// NewSolver must always configure a bounded ReCaptcha wait. Left alone the
// 2captcha client polls for 600s, pinning a browser page and its proxy lane
// for ten minutes — long after the caller's own timeout has given up.
func TestNewSolverBoundsTheRecaptchaWait(t *testing.T) {
	defaultSeconds := int(DefaultCaptchaSolveTimeout / time.Second)

	cases := map[string]struct {
		given time.Duration
		want  int
	}{
		"explicit": {given: 45 * time.Second, want: 45},
		"zero":     {given: 0, want: defaultSeconds},
		"negative": {given: -1 * time.Second, want: defaultSeconds},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			solver := NewSolver("api-key", tc.given)
			client, ok := solver.client.(*api2captcha.Client)
			if !ok {
				t.Fatalf("solver client is %T, want *api2captcha.Client", solver.client)
			}
			if client.RecaptchaTimeout != tc.want {
				t.Fatalf("RecaptchaTimeout = %d, want %d", client.RecaptchaTimeout, tc.want)
			}
		})
	}
}

// The library's own default is the hazard this guards against; if a dependency
// bump ever made it shorter than ours, ours would be the pessimisation.
func TestDefaultCaptchaSolveTimeoutIsShorterThanTheLibrarys(t *testing.T) {
	libraryDefault := api2captcha.NewClient("api-key").RecaptchaTimeout
	if int(DefaultCaptchaSolveTimeout/time.Second) >= libraryDefault {
		t.Fatalf("default %s does not bound the library's %ds wait",
			DefaultCaptchaSolveTimeout, libraryDefault)
	}
}
