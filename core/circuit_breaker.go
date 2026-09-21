package core

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

type CircuitBreakerConfig struct {
	FailureThreshold int
	RecoveryTimeout  time.Duration
	SuccessThreshold int
}

func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		RecoveryTimeout:  60 * time.Second,
		SuccessThreshold: 2,
	}
}

// SearchCapability names the kind of request a breaker tracks.
//
// An engine can serve one capability perfectly while the other is dead:
// DuckDuckGo's image page became a JS-only shell while its web search kept
// answering in ~1.2s. Pooling both under the engine name hides the dead half
// behind the healthy half's successes, so the breaker never opens and /health
// keeps reporting the engine "ready" — which is how a broken capability can
// go on taking traffic at ~12s a request until a human notices.
type SearchCapability string

const (
	CapabilitySearch SearchCapability = "search"
	CapabilityImage  SearchCapability = "image"
)

// CapabilityFor maps the isImage flag the search paths already carry onto a
// capability name.
func CapabilityFor(isImage bool) SearchCapability {
	if isImage {
		return CapabilityImage
	}
	return CapabilitySearch
}

func (c SearchCapability) String() string { return string(c) }

// CircuitBreaker tracks failure state for one engine capability.
type CircuitBreaker struct {
	mu              sync.RWMutex
	name            string
	capability      SearchCapability
	state           CircuitState
	config          CircuitBreakerConfig
	failureCount    int
	successCount    int
	successLatency  time.Duration
	successSamples  int64
	lastFailureTime time.Time
	lastStateChange time.Time
}

func NewCircuitBreaker(name string, cfg CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		name:            name,
		state:           CircuitClosed,
		config:          cfg,
		lastStateChange: time.Now(),
	}
}

// NewCapabilityCircuitBreaker builds a breaker scoped to one capability of one
// engine, so a dead capability trips on its own evidence.
func NewCapabilityCircuitBreaker(engineName string, capability SearchCapability, cfg CircuitBreakerConfig) *CircuitBreaker {
	cb := NewCircuitBreaker(engineName, cfg)
	cb.capability = capability
	return cb
}

// Capability reports which capability this breaker tracks, empty for a breaker
// built without one.
func (cb *CircuitBreaker) Capability() SearchCapability {
	return cb.capability
}

func (cb *CircuitBreaker) AllowRequest(ctx context.Context) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.lastFailureTime) >= cb.config.RecoveryTimeout {
			cb.setState(CircuitHalfOpen)
			WithRequestEngine(ctx, cb.name).Info("Recovery timeout elapsed, moving to half-open")
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	default:
		return true
	}
}

func (cb *CircuitBreaker) RecordSuccess(ctx context.Context) {
	cb.RecordSuccessDuration(ctx, 0)
}

func (cb *CircuitBreaker) RecordSuccessDuration(ctx context.Context, elapsed time.Duration) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if elapsed > 0 {
		cb.successLatency += elapsed
		cb.successSamples++
	}

	switch cb.state {
	case CircuitHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.setState(CircuitClosed)
			cb.failureCount = 0
			cb.successCount = 0
			WithRequestEngine(ctx, cb.name).Info("Circuit recovered, closed")
		}
	case CircuitClosed:
		cb.failureCount = 0
	}
}

func (cb *CircuitBreaker) RecordFailure(ctx context.Context) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		cb.failureCount++
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.setState(CircuitOpen)
			WithRequestEngine(ctx, cb.name).
				WithField("failure_count", cb.failureCount).
				WithField("recovery_timeout", cb.config.RecoveryTimeout.String()).
				Warn("Circuit opened after consecutive failures")
		}
	case CircuitHalfOpen:
		cb.setState(CircuitOpen)
		cb.successCount = 0
		WithRequestEngine(ctx, cb.name).Warn("Failed during half-open, circuit re-opened")
	}
}

func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) Stats() map[string]interface{} {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	stats := map[string]interface{}{
		"engine":        cb.name,
		"state":         cb.state.String(),
		"failure_count": cb.failureCount,
		"last_changed":  cb.lastStateChange.Format(time.RFC3339),
	}
	if cb.capability != "" {
		stats["capability"] = cb.capability.String()
	}

	if cb.state == CircuitOpen {
		remaining := cb.config.RecoveryTimeout - time.Since(cb.lastFailureTime)
		if remaining < 0 {
			remaining = 0
		}

		// Expose retry_in as integer seconds for easier client-side processing.
		retryInSeconds := int64(0)
		if remaining > 0 {
			retryInSeconds = int64((remaining + time.Second - time.Nanosecond) / time.Second)
		}
		stats["retry_in"] = retryInSeconds
	}
	if cb.successSamples > 0 {
		stats["avg_response_ms"] = int64((cb.successLatency / time.Duration(cb.successSamples)) / time.Millisecond)
	}

	return stats
}

func (cb *CircuitBreaker) AvgSuccessLatency() (time.Duration, bool) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	if cb.successSamples == 0 {
		return 0, false
	}
	return cb.successLatency / time.Duration(cb.successSamples), true
}

func (cb *CircuitBreaker) setState(state CircuitState) {
	cb.state = state
	cb.lastStateChange = time.Now()
}

type CircuitBreakerManager struct {
	mu       sync.RWMutex
	breakers map[string]*CircuitBreaker
	config   CircuitBreakerConfig
}

func NewCircuitBreakerManager(cfg CircuitBreakerConfig) *CircuitBreakerManager {
	return &CircuitBreakerManager{
		breakers: make(map[string]*CircuitBreaker),
		config:   cfg,
	}
}

// Get returns the breaker for one capability of one engine, creating it on
// first use. Keying by capability is what lets a dead image endpoint trip
// while the same engine's web search keeps serving.
func (m *CircuitBreakerManager) Get(engineName string, capability SearchCapability) *CircuitBreaker {
	key := breakerKey(engineName, capability)

	m.mu.RLock()
	if cb, ok := m.breakers[key]; ok {
		m.mu.RUnlock()
		return cb
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if cb, ok := m.breakers[key]; ok {
		return cb
	}

	cb := NewCapabilityCircuitBreaker(engineName, capability, m.config)
	m.breakers[key] = cb
	return cb
}

func breakerKey(engineName string, capability SearchCapability) string {
	if capability == "" {
		return engineName
	}
	return engineName + ":" + capability.String()
}

func (m *CircuitBreakerManager) AllStats() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make([]map[string]interface{}, 0, len(m.breakers))
	for _, cb := range m.breakers {
		stats = append(stats, cb.Stats())
	}
	return stats
}

var ErrCircuitOpen = fmt.Errorf("circuit breaker is open - engine temporarily disabled")
