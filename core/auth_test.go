package core

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func authTestApp(cfg AuthConfig) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: JSONErrorMiddleware()})
	app.Use(AuthMiddleware(cfg))
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendString("ok") })
	app.Get("/docs/index.html", func(c *fiber.Ctx) error { return c.SendString("docs") })
	app.Get("/duckduckgo/search", func(c *fiber.Ctx) error { return c.SendString("results") })
	return app
}

func statusFor(t *testing.T, app *fiber.App, path string, headers map[string]string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestAuthDisabledWithoutKeys(t *testing.T) {
	// An unconfigured local run must behave exactly as it did before.
	cfg := AuthConfig{}
	if cfg.Enabled() {
		t.Fatal("no configured key should leave authentication disabled")
	}
	if cfg := (AuthConfig{APIKeys: []string{"   ", ""}}); cfg.Enabled() {
		t.Fatal("blank keys should not enable authentication")
	}
}

func TestAuthRejectsMissingAndWrongKeys(t *testing.T) {
	app := authTestApp(AuthConfig{APIKeys: []string{"correct-horse"}})

	if got := statusFor(t, app, "/duckduckgo/search", nil); got != http.StatusUnauthorized {
		t.Fatalf("no key: got %d, want 401", got)
	}
	wrong := map[string]string{DefaultAuthHeader: "battery-staple"}
	if got := statusFor(t, app, "/duckduckgo/search", wrong); got != http.StatusUnauthorized {
		t.Fatalf("wrong key: got %d, want 401", got)
	}
}

func TestAuthAcceptsHeaderAndBearer(t *testing.T) {
	app := authTestApp(AuthConfig{APIKeys: []string{"correct-horse", "second-key"}})

	for name, headers := range map[string]map[string]string{
		"api key header": {DefaultAuthHeader: "correct-horse"},
		"second key":     {DefaultAuthHeader: "second-key"},
		"bearer token":   {fiber.HeaderAuthorization: "Bearer correct-horse"},
	} {
		if got := statusFor(t, app, "/duckduckgo/search", headers); got != http.StatusOK {
			t.Fatalf("%s: got %d, want 200", name, got)
		}
	}
}

func TestAuthLeavesProbesPublic(t *testing.T) {
	// An orchestrator's liveness and readiness probes hold no credentials.
	app := authTestApp(AuthConfig{APIKeys: []string{"correct-horse"}})

	for _, path := range []string{"/health", "/docs/index.html"} {
		if got := statusFor(t, app, path, nil); got != http.StatusOK {
			t.Fatalf("%s should stay public: got %d", path, got)
		}
	}
}

func TestAuthPublicPrefixDoesNotLeak(t *testing.T) {
	// "/docs" must not make "/docsearch" public.
	cfg := AuthConfig{APIKeys: []string{"k"}, PublicPaths: []string{"/docs"}}

	if !cfg.isPublic("/docs") || !cfg.isPublic("/docs/") || !cfg.isPublic("/docs/index.html") {
		t.Fatal("/docs and its children should be public")
	}
	if cfg.isPublic("/docsearch") {
		t.Fatal("/docsearch must not be treated as public")
	}
}

func TestAuthUsesConfiguredHeader(t *testing.T) {
	app := authTestApp(AuthConfig{
		APIKeys:    []string{"correct-horse"},
		HeaderName: "X-Tenant-Key",
	})

	custom := map[string]string{"X-Tenant-Key": "correct-horse"}
	if got := statusFor(t, app, "/duckduckgo/search", custom); got != http.StatusOK {
		t.Fatalf("configured header: got %d, want 200", got)
	}
	def := map[string]string{DefaultAuthHeader: "correct-horse"}
	if got := statusFor(t, app, "/duckduckgo/search", def); got != http.StatusUnauthorized {
		t.Fatalf("default header should not be accepted: got %d, want 401", got)
	}
}
