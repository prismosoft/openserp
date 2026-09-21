package core

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api2captcha "github.com/2captcha/2captcha-go"
)

func testAPIClient(t *testing.T, handler http.HandlerFunc) *twoCaptchaAPIClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := newTwoCaptchaAPIClient("api-key", 5*time.Second)
	client.baseURL = server.URL
	client.pollInterval = time.Millisecond
	return client
}

func TestTwoCaptchaClientSubmitsAndPollsUntilSolved(t *testing.T) {
	var polls atomic.Int32
	client := testAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/in.php":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			if got := r.Form.Get("key"); got != "api-key" {
				t.Errorf("key = %q", got)
			}
			// The proxy the worker should solve through rides in the params.
			if got := r.Form.Get("proxy"); got != "user:pass@host:8080" {
				t.Errorf("proxy param = %q", got)
			}
			fmt.Fprint(w, `{"status":1,"request":"job-1"}`)
		case "/res.php":
			if got := r.URL.Query().Get("id"); got != "job-1" {
				t.Errorf("polled id = %q", got)
			}
			if polls.Add(1) < 3 {
				fmt.Fprint(w, `{"status":0,"request":"CAPCHA_NOT_READY"}`)
				return
			}
			fmt.Fprint(w, `{"status":1,"request":"solved-token"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	captcha := api2captcha.ReCaptcha{SiteKey: "sk", Url: "https://example.test"}
	req := captcha.ToRequest()
	req.SetProxy("HTTP", "user:pass@host:8080")

	token, id, err := client.Solve(req, "")
	if err != nil {
		t.Fatalf("solve: %v", err)
	}
	if token != "solved-token" || id != "job-1" {
		t.Fatalf("token=%q id=%q", token, id)
	}
	if polls.Load() < 3 {
		t.Fatalf("expected polling until ready, got %d polls", polls.Load())
	}
}

// The whole reason for replacing api2captcha: it collapsed every transport
// error to a bare "Network failure" with the cause discarded, which is why a
// solver that never worked gave no evidence of why.
func TestTwoCaptchaClientKeepsTheTransportErrorCause(t *testing.T) {
	client := newTwoCaptchaAPIClient("api-key", 2*time.Second)
	client.baseURL = "http://127.0.0.1:1" // nothing listens here
	client.pollInterval = time.Millisecond

	_, _, err := client.Solve(newTestReCaptchaRequest(), "")
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), "Network failure") {
		t.Fatalf("error was flattened to the library's opaque message: %v", err)
	}
	if !strings.Contains(err.Error(), "submit captcha") || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("error lost the cause: %v", err)
	}
}

// An API error must be reported as itself, not as a timeout or a network fault.
func TestTwoCaptchaClientReportsAPIRejections(t *testing.T) {
	client := testAPIClient(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":0,"request":"ERROR_WRONG_USER_KEY","error_text":"bad key"}`)
	})

	_, _, err := client.Solve(newTestReCaptchaRequest(), "")
	if err == nil {
		t.Fatal("expected a rejection")
	}
	if !strings.Contains(err.Error(), "ERROR_WRONG_USER_KEY") || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("rejection lost its detail: %v", err)
	}
}

// The API call itself must egress through the lane's proxy, so 2captcha does
// not see a datacenter IP for a residential lane's work.
func TestTwoCaptchaClientSendsTheAPICallThroughTheProxy(t *testing.T) {
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		// An absolute request URI is what a forward proxy receives.
		if !strings.HasPrefix(r.RequestURI, "http://") {
			t.Errorf("proxy got a non-absolute URI: %q", r.RequestURI)
		}
		fmt.Fprint(w, `{"status":1,"request":"job-1"}`)
	}))
	t.Cleanup(proxy.Close)

	client := newTwoCaptchaAPIClient("api-key", time.Second)
	client.baseURL = "http://2captcha.invalid"
	client.pollInterval = time.Millisecond

	_, _, _ = client.Solve(newTestReCaptchaRequest(), proxy.URL)

	if proxied.Load() == 0 {
		t.Fatal("the 2captcha API call did not go through the proxy")
	}
}

func TestTwoCaptchaClientRejectsAMalformedProxy(t *testing.T) {
	client := newTwoCaptchaAPIClient("api-key", time.Second)
	if _, _, err := client.Solve(newTestReCaptchaRequest(), "://nonsense"); err == nil {
		t.Fatal("a malformed proxy should fail loudly, not silently go direct")
	}
}

func newTestReCaptchaRequest() api2captcha.Request {
	captcha := api2captcha.ReCaptcha{SiteKey: "sk", Url: "https://example.test"}
	return captcha.ToRequest()
}
