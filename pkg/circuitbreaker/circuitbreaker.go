// Package circuitbreaker implements the Circuit Breaker pattern for fault tolerance
package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

// State represents the current state of the circuit breaker
type State int

const (
	// StateClosed means the circuit is functioning normally
	StateClosed State = iota
	// StateOpen means the circuit is broken and requests are rejected
	StateOpen
	// StateHalfOpen means the circuit is testing if the service recovered
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// ErrCircuitOpen is returned when the circuit breaker is open
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	mu sync.RWMutex

	name            string
	state           State
	failureCount    int
	successCount    int
	lastFailure     time.Time
	lastStateChange time.Time

	// Configuration
	failureThreshold int           // Number of failures before opening
	successThreshold int           // Number of successes in half-open before closing
	timeout          time.Duration // How long to wait before trying again

	// Callbacks
	onStateChange func(name string, from, to State)
}

// Config holds circuit breaker configuration
type Config struct {
	Name             string
	FailureThreshold int           // Default: 3
	SuccessThreshold int           // Default: 1
	Timeout          time.Duration // Default: 30 seconds
	OnStateChange    func(name string, from, to State)
}

// New creates a new circuit breaker
func New(cfg Config) *CircuitBreaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 3
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 1
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &CircuitBreaker{
		name:             cfg.Name,
		state:            StateClosed,
		failureThreshold: cfg.FailureThreshold,
		successThreshold: cfg.SuccessThreshold,
		timeout:          cfg.Timeout,
		onStateChange:    cfg.OnStateChange,
		lastStateChange:  time.Now(),
	}
}

// Execute runs the given function with circuit breaker protection
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.AllowRequest() {
		return ErrCircuitOpen
	}

	err := fn()

	if err != nil {
		cb.RecordFailure()
		return err
	}

	cb.RecordSuccess()
	return nil
}

// AllowRequest checks if a request should be allowed through
func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		// Check if timeout has elapsed
		if time.Since(cb.lastFailure) >= cb.timeout {
			cb.transitionTo(StateHalfOpen)
			return true
		}
		return false
	case StateHalfOpen:
		// Allow limited requests in half-open state
		return true
	default:
		return false
	}
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		// Reset failure count on success
		cb.failureCount = 0
	case StateHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.successThreshold {
			cb.transitionTo(StateClosed)
		}
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailure = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failureCount >= cb.failureThreshold {
			cb.transitionTo(StateOpen)
		}
	case StateHalfOpen:
		// Any failure in half-open goes back to open
		cb.transitionTo(StateOpen)
	}
}

// State returns the current state of the circuit breaker
func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.transitionTo(StateClosed)
	cb.failureCount = 0
	cb.successCount = 0
}

// Stats returns current circuit breaker statistics
func (cb *CircuitBreaker) Stats() Stats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return Stats{
		Name:            cb.name,
		State:           cb.state,
		FailureCount:    cb.failureCount,
		SuccessCount:    cb.successCount,
		LastFailure:     cb.lastFailure,
		LastStateChange: cb.lastStateChange,
	}
}

// Stats holds circuit breaker statistics
type Stats struct {
	Name            string
	State           State
	FailureCount    int
	SuccessCount    int
	LastFailure     time.Time
	LastStateChange time.Time
}

func (cb *CircuitBreaker) transitionTo(newState State) {
	if cb.state == newState {
		return
	}

	oldState := cb.state
	cb.state = newState
	cb.lastStateChange = time.Now()
	cb.failureCount = 0
	cb.successCount = 0

	if cb.onStateChange != nil {
		go cb.onStateChange(cb.name, oldState, newState)
	}
}
