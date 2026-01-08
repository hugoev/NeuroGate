package circuitbreaker

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCircuitBreaker_StartsInClosedState(t *testing.T) {
	cb := New(Config{Name: "test"})

	if cb.State() != StateClosed {
		t.Errorf("expected state to be closed, got %v", cb.State())
	}
}

func TestCircuitBreaker_OpensAfterFailures(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 3,
		Timeout:          100 * time.Millisecond,
	})

	// Simulate 3 failures
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != StateOpen {
		t.Errorf("expected state to be open after 3 failures, got %v", cb.State())
	}
}

func TestCircuitBreaker_RejectsRequestsWhenOpen(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 1,
		Timeout:          100 * time.Millisecond,
	})

	cb.RecordFailure() // This should open the circuit

	if cb.AllowRequest() {
		t.Error("expected request to be rejected when circuit is open")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpenAfterTimeout(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 1,
		Timeout:          50 * time.Millisecond,
	})

	cb.RecordFailure() // Open the circuit

	if cb.State() != StateOpen {
		t.Fatalf("expected circuit to be open, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(60 * time.Millisecond)

	// This should transition to half-open
	if !cb.AllowRequest() {
		t.Error("expected request to be allowed after timeout (half-open)")
	}

	if cb.State() != StateHalfOpen {
		t.Errorf("expected state to be half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_ClosesAfterSuccessInHalfOpen(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 1,
		SuccessThreshold: 1,
		Timeout:          50 * time.Millisecond,
	})

	cb.RecordFailure() // Open
	time.Sleep(60 * time.Millisecond)
	cb.AllowRequest()  // Transition to half-open
	cb.RecordSuccess() // Should close

	if cb.State() != StateClosed {
		t.Errorf("expected state to be closed after success in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_ReopensOnFailureInHalfOpen(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 1,
		Timeout:          50 * time.Millisecond,
	})

	cb.RecordFailure() // Open
	time.Sleep(60 * time.Millisecond)
	cb.AllowRequest()  // Transition to half-open
	cb.RecordFailure() // Should reopen

	if cb.State() != StateOpen {
		t.Errorf("expected state to be open after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreaker_Execute_Success(t *testing.T) {
	cb := New(Config{Name: "test"})

	err := cb.Execute(func() error {
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCircuitBreaker_Execute_Failure(t *testing.T) {
	cb := New(Config{Name: "test"})

	expectedErr := errors.New("test error")
	err := cb.Execute(func() error {
		return expectedErr
	})

	if err != expectedErr {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
}

func TestCircuitBreaker_Execute_RejectsWhenOpen(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 1,
		Timeout:          1 * time.Second,
	})

	// Open the circuit
	cb.RecordFailure()

	err := cb.Execute(func() error {
		return nil
	})

	if err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreaker_OnStateChange(t *testing.T) {
	stateChanges := make(chan struct {
		from, to State
	}, 10)

	cb := New(Config{
		Name:             "test",
		FailureThreshold: 1,
		Timeout:          50 * time.Millisecond,
		OnStateChange: func(name string, from, to State) {
			stateChanges <- struct {
				from, to State
			}{from, to}
		},
	})

	cb.RecordFailure() // Should trigger closed -> open

	select {
	case change := <-stateChanges:
		if change.from != StateClosed || change.to != StateOpen {
			t.Errorf("expected closed->open, got %v->%v", change.from, change.to)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected state change callback")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 1,
	})

	cb.RecordFailure() // Open

	if cb.State() != StateOpen {
		t.Fatalf("expected circuit to be open, got %v", cb.State())
	}

	cb.Reset()

	if cb.State() != StateClosed {
		t.Errorf("expected circuit to be closed after reset, got %v", cb.State())
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := New(Config{
		Name:             "test",
		FailureThreshold: 100,
		Timeout:          1 * time.Second,
	})

	var wg sync.WaitGroup

	// Simulate concurrent access
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			cb.AllowRequest()
			cb.RecordSuccess()
			cb.RecordFailure()
			cb.State()
			cb.Stats()
		}()
	}

	wg.Wait()
	// If we get here without a race condition, the test passes
}

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.expected)
		}
	}
}
