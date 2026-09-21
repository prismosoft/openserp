package core

import (
	"context"
	"testing"
)

// The failure this guards against: DuckDuckGo's image page went JS-only while
// its web search kept answering in ~1.2s. With one breaker per engine, the web
// successes kept the pooled breaker closed, so the dead image endpoint went on
// taking traffic at ~12s a request until a human noticed in a slow pilot.
func TestADeadCapabilityTripsWithoutTakingTheHealthyOneDown(t *testing.T) {
	cfg := DefaultCircuitBreakerConfig()
	cfg.FailureThreshold = 2
	mgr := NewCircuitBreakerManager(cfg)
	ctx := context.Background()

	image := mgr.Get("duckduckgo", CapabilityImage)
	for range cfg.FailureThreshold {
		image.RecordFailure(ctx)
	}

	web := mgr.Get("duckduckgo", CapabilitySearch)
	web.RecordSuccess(ctx)

	if image.State() != CircuitOpen {
		t.Fatalf("image breaker = %v, want open", image.State())
	}
	if web.State() != CircuitClosed {
		t.Fatalf("web breaker = %v, want closed — a dead capability must not close the engine", web.State())
	}
	if !web.AllowRequest(ctx) {
		t.Fatal("web search must keep serving while image is tripped")
	}
	if image.AllowRequest(ctx) {
		t.Fatal("image must stop taking traffic once tripped")
	}
}

// Stats must name the capability as its own field; a consumer that had to
// parse a composed key would break the moment an engine name contained ":".
func TestBreakerStatsNameTheCapability(t *testing.T) {
	mgr := NewCircuitBreakerManager(DefaultCircuitBreakerConfig())
	mgr.Get("google", CapabilityImage)

	stats := mgr.AllStats()
	if len(stats) != 1 {
		t.Fatalf("got %d breakers, want 1", len(stats))
	}
	if engine, _ := stats[0]["engine"].(string); engine != "google" {
		t.Fatalf("engine = %q, want the bare engine name", engine)
	}
	if capability, _ := stats[0]["capability"].(string); capability != "image" {
		t.Fatalf("capability = %q, want \"image\"", capability)
	}
}

// A breaker predating capability keying carries no capability and must still
// apply to the engine as a whole rather than being silently ignored.
func TestCircuitOpenForHandlesBothKeyedAndUnkeyedBreakers(t *testing.T) {
	keyed := []map[string]interface{}{
		{"engine": "duckduckgo", "capability": "image", "state": "open"},
		{"engine": "duckduckgo", "capability": "search", "state": "closed"},
	}
	if !circuitOpenFor(keyed, "duckduckgo", CapabilityImage) {
		t.Error("image should read as open")
	}
	if circuitOpenFor(keyed, "duckduckgo", CapabilitySearch) {
		t.Error("search should read as closed")
	}
	if circuitOpenFor(keyed, "bing", CapabilityImage) {
		t.Error("another engine must not inherit the state")
	}

	unkeyed := []map[string]interface{}{{"engine": "ecosia", "state": "open"}}
	for _, capability := range []SearchCapability{CapabilitySearch, CapabilityImage} {
		if !circuitOpenFor(unkeyed, "ecosia", capability) {
			t.Errorf("an unkeyed open breaker must apply to %s", capability)
		}
	}

	// No breaker yet means the capability has simply not been used.
	if circuitOpenFor(nil, "google", CapabilityImage) {
		t.Error("an absent breaker must read as healthy, not open")
	}
}
