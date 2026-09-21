package core

import (
	"crypto/subtle"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// AuthConfig configures API key authentication for the HTTP server.
//
// A search API is a browser farm pointed at other people's sites. Left open it
// is someone else's scraping proxy running on your bill, and the abuse arrives
// as rate limiting and IP blocks on the engines you depend on, so the damage
// lands on your own searches rather than on the abuser.
type AuthConfig struct {
	// APIKeys are the accepted credentials. Authentication is enforced only
	// when at least one is configured, so an unconfigured local run behaves
	// exactly as before.
	APIKeys []string
	// HeaderName carries the key. Defaults to DefaultAuthHeader.
	HeaderName string
	// PublicPaths are served without a key. Liveness and readiness probes have
	// to answer an orchestrator that holds no credentials.
	PublicPaths []string
}

const (
	// DefaultAuthHeader is the header clients send their key in.
	DefaultAuthHeader = "X-API-Key"
	// AuthErrorCode is the machine-readable code returned to an unauthenticated
	// caller.
	AuthErrorCode = "unauthorized"
)

// DefaultPublicPaths are reachable without a key: an orchestrator's health and
// readiness probes, and the API description itself.
func DefaultPublicPaths() []string {
	return []string{"/health", "/ready", "/openapi.yaml", "/docs"}
}

// Enabled reports whether any key is configured. With none, the server stays
// open and says so at startup rather than silently rejecting every request.
func (c AuthConfig) Enabled() bool {
	for _, key := range c.APIKeys {
		if strings.TrimSpace(key) != "" {
			return true
		}
	}
	return false
}

func (c AuthConfig) headerName() string {
	if name := strings.TrimSpace(c.HeaderName); name != "" {
		return name
	}
	return DefaultAuthHeader
}

func (c AuthConfig) publicPaths() []string {
	if len(c.PublicPaths) > 0 {
		return c.PublicPaths
	}
	return DefaultPublicPaths()
}

// isPublic matches a path exactly or as a prefix segment, so "/docs" also
// covers "/docs/" and "/docs/index.html" without matching "/docsearch".
func (c AuthConfig) isPublic(path string) bool {
	trimmed := strings.TrimSuffix(path, "/")
	for _, public := range c.publicPaths() {
		candidate := strings.TrimSuffix(public, "/")
		if trimmed == candidate || strings.HasPrefix(path, candidate+"/") {
			return true
		}
	}
	return false
}

// matches compares in constant time, so a wrong key cannot be recovered by
// timing how long the comparison took.
func (c AuthConfig) matches(presented string) bool {
	if presented == "" {
		return false
	}
	valid := false
	for _, key := range c.APIKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(key), []byte(presented)) == 1 {
			valid = true
		}
	}
	return valid
}

// AuthMiddleware rejects a request that carries no valid API key.
//
// The key may arrive in the configured header or as a bearer token, because a
// caller that already speaks OAuth-shaped HTTP should not need a special case.
// Mount it after the request logger so a rejected request is still logged with
// its request ID.
func AuthMiddleware(cfg AuthConfig) fiber.Handler {
	headerName := cfg.headerName()
	return func(c *fiber.Ctx) error {
		if cfg.isPublic(c.Path()) {
			return c.Next()
		}
		presented := strings.TrimSpace(c.Get(headerName))
		if presented == "" {
			authorization := strings.TrimSpace(c.Get(fiber.HeaderAuthorization))
			if after, found := strings.CutPrefix(authorization, "Bearer "); found {
				presented = strings.TrimSpace(after)
			}
		}
		if !cfg.matches(presented) {
			return &APIError{
				HTTPStatus: fiber.StatusUnauthorized,
				ErrorCode:  AuthErrorCode,
				Reason:     "missing_or_invalid_api_key",
				Message:    "A valid " + headerName + " header is required.",
			}
		}
		return c.Next()
	}
}
