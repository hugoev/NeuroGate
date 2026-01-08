// Package health provides health checking utilities for services
package health

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Status represents the health status of a service
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusUnhealthy Status = "unhealthy"
	StatusDegraded  Status = "degraded"
)

// Check represents a single health check
type Check struct {
	Name    string        `json:"name"`
	Status  Status        `json:"status"`
	Message string        `json:"message,omitempty"`
	Latency time.Duration `json:"latency_ms,omitempty"`
}

// Response represents the health check response
type Response struct {
	Status    Status            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Version   string            `json:"version"`
	Checks    map[string]*Check `json:"checks,omitempty"`
}

// Checker manages health checks for a service
type Checker struct {
	mu      sync.RWMutex
	version string
	checks  map[string]CheckFunc
	results map[string]*Check
}

// CheckFunc is a function that performs a health check
type CheckFunc func(ctx context.Context) *Check

// NewChecker creates a new health checker
func NewChecker(version string) *Checker {
	return &Checker{
		version: version,
		checks:  make(map[string]CheckFunc),
		results: make(map[string]*Check),
	}
}

// Register adds a health check
func (h *Checker) Register(name string, check CheckFunc) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = check
}

// Run executes all health checks
func (h *Checker) Run(ctx context.Context) *Response {
	h.mu.Lock()
	defer h.mu.Unlock()

	response := &Response{
		Status:    StatusHealthy,
		Timestamp: time.Now(),
		Version:   h.version,
		Checks:    make(map[string]*Check),
	}

	for name, checkFn := range h.checks {
		result := checkFn(ctx)
		response.Checks[name] = result

		// Update overall status based on individual checks
		switch result.Status {
		case StatusUnhealthy:
			response.Status = StatusUnhealthy
		case StatusDegraded:
			if response.Status == StatusHealthy {
				response.Status = StatusDegraded
			}
		}
	}

	return response
}

// HTTPHandler returns an HTTP handler for health checks
func (h *Checker) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		response := h.Run(ctx)

		statusCode := http.StatusOK
		if response.Status == StatusUnhealthy {
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)

		// Simple JSON encoding
		w.Write([]byte(`{"status":"` + string(response.Status) + `","version":"` + response.Version + `"}`))
	}
}

// HTTPCheck creates a health check for an HTTP endpoint
func HTTPCheck(name string, url string, timeout time.Duration) CheckFunc {
	return func(ctx context.Context) *Check {
		start := time.Now()

		client := &http.Client{Timeout: timeout}
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return &Check{
				Name:    name,
				Status:  StatusUnhealthy,
				Message: err.Error(),
				Latency: time.Since(start),
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			return &Check{
				Name:    name,
				Status:  StatusUnhealthy,
				Message: err.Error(),
				Latency: time.Since(start),
			}
		}
		defer resp.Body.Close()

		latency := time.Since(start)

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return &Check{
				Name:    name,
				Status:  StatusHealthy,
				Latency: latency,
			}
		}

		return &Check{
			Name:    name,
			Status:  StatusUnhealthy,
			Message: "unhealthy status code: " + resp.Status,
			Latency: latency,
		}
	}
}
