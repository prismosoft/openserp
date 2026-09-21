package google

import (
	"strings"
	"testing"
)

// rod's Eval calls .apply on whatever the source evaluates to, so the source
// must BE a function. The previous injection was `;(() => {...})();`, which
// evaluates to undefined, and every solved token died with
// "TypeError: Cannot read properties of undefined (reading 'apply')" before it
// reached the page — after being paid for.
func TestCaptchaSubmitJSIsAFunctionExpression(t *testing.T) {
	js := strings.TrimSpace(captchaSubmitJS)

	if !strings.HasPrefix(js, "(token) =>") {
		t.Fatalf("source must be a function expression rod can apply, got: %.40s", js)
	}
	if strings.HasPrefix(js, ";") || strings.HasSuffix(js, "();") {
		t.Fatal("source is self-invoking again; it would evaluate to undefined")
	}
}

// The token arrives as an argument, never spliced into the source, so a token
// containing a quote cannot break the script.
func TestCaptchaSubmitJSTakesTheTokenAsAnArgument(t *testing.T) {
	if strings.Contains(captchaSubmitJS, "%s") {
		t.Fatal("token is still formatted into the source")
	}
	if !strings.Contains(captchaSubmitJS, "field.value = token") {
		t.Fatal("token argument is not assigned to the response field")
	}
}

// Google's sorry page has changed shape before. Without a fallback, a missing
// submitCallback silently wastes every solve, so both routes must exist and
// the script must report which one it took.
func TestCaptchaSubmitJSFallsBackWhenSubmitCallbackIsMissing(t *testing.T) {
	for _, needle := range []string{
		`typeof submitCallback === "function"`,
		`return "submitCallback"`,
		`form.requestSubmit`,
		`return "form"`,
		`return "none"`,
	} {
		if !strings.Contains(captchaSubmitJS, needle) {
			t.Errorf("submit script is missing %q", needle)
		}
	}
}
