package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreaker is a simple state machine to prevent cascading failures
type CircuitBreaker struct {
	mu           sync.RWMutex
	state        State
	failureCount int
	maxFailures  int
	resetTimeout time.Duration
	lastFailure  time.Time
}

// New returns a new CircuitBreaker
func New(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:        StateClosed,
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
	}
}

// Execute runs the given function if the circuit is closed or half-open.
// If the circuit is open, it returns ErrCircuitOpen immediately.
func (cb *CircuitBreaker) Execute(req func() error) error {
	cb.mu.RLock()
	state := cb.state
	lastFailure := cb.lastFailure
	cb.mu.RUnlock()

	if state == StateOpen {
		if time.Since(lastFailure) > cb.resetTimeout {
			cb.mu.Lock()
			cb.state = StateHalfOpen
			cb.mu.Unlock()
		} else {
			return ErrCircuitOpen
		}
	}

	err := req()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failureCount++
		cb.lastFailure = time.Now()
		if cb.failureCount >= cb.maxFailures {
			cb.state = StateOpen
		}
	} else {
		// Success resets everything
		cb.failureCount = 0
		cb.state = StateClosed
	}

	return err
}
