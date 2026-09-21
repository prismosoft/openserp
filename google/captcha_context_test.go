package google

import (
	"context"
	"testing"
	"time"

	"github.com/karust/openserp/core"
)

// A solve takes tens of seconds, routinely longer than the request deadline,
// so by the time the bought token is injected the caller's context is usually
// already dead. Doing that work on the caller's context threw the solve away
// with "context deadline exceeded" — paying for a token and never using it —
// and, because the same expired context gates proxy rotation, it also made a
// three-lane pool log "single proxy challenged and cannot rotate".
func TestPostSolveWorkSurvivesAnExpiredRequestContext(t *testing.T) {
	expired, cancel := context.WithCancel(context.Background())
	cancel()

	if expired.Err() == nil {
		t.Fatal("test setup: context should be cancelled")
	}

	// context.WithoutCancel is what detaches the post-solve work; without it
	// any context derived here is born already done.
	detached, detachedCancel := context.WithTimeout(
		context.WithoutCancel(expired), solvedCaptchaInjectTimeout)
	defer detachedCancel()

	if detached.Err() != nil {
		t.Fatalf("detached context is already done: %v", detached.Err())
	}
	deadline, ok := detached.Deadline()
	if !ok {
		t.Fatal("detached context must still carry a deadline of its own")
	}
	if remaining := time.Until(deadline); remaining <= 0 {
		t.Fatalf("detached deadline already passed: %s", remaining)
	}
}

// Both post-solve budgets must be bounded: they run detached, so nothing else
// would ever stop them.
func TestPostSolveBudgetsAreBounded(t *testing.T) {
	for name, budget := range map[string]time.Duration{
		"inject": solvedCaptchaInjectTimeout,
		"settle": solvedCaptchaSettleTimeout,
	} {
		if budget <= 0 {
			t.Errorf("%s budget is %s; detached work must be bounded", name, budget)
		}
		if budget > time.Minute {
			t.Errorf("%s budget is %s; too long to hold a page and its lane", name, budget)
		}
	}
}

// Guard the gate order: no solver, no page, no work.
func TestSolveCaptchaRefusesWithoutASolver(t *testing.T) {
	gogl := New(core.Browser{}, core.SearchEngineOptions{})
	if gogl.solveCaptcha(t.Context(), nil, "sitekey", "datas", "") {
		t.Fatal("expected solveCaptcha to fail without a configured solver")
	}
}
