// Gateway Service - REST API Load Balancer for LLM Workers
// Implements Round Robin load balancing and Circuit Breaker pattern
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	llmv1 "github.com/hugovillarreal/neurogate/api/proto/llm/v1"
	"github.com/hugovillarreal/neurogate/pkg/circuitbreaker"
	"github.com/hugovillarreal/neurogate/pkg/health"
	"github.com/hugovillarreal/neurogate/pkg/logger"
	"github.com/hugovillarreal/neurogate/pkg/metrics"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultHTTPPort    = "8080"
	defaultMetricsPort = "9091"
	version            = "1.0.0"
)

// Worker represents a backend worker node
type Worker struct {
	ID      string
	Address string
	Conn    *grpc.ClientConn
	Client  llmv1.LLMServiceClient
	CB      *circuitbreaker.CircuitBreaker
	Healthy atomic.Bool
}

// Gateway is the main load balancer
type Gateway struct {
	log           *logger.Logger
	metrics       *metrics.Metrics
	healthChecker *health.Checker

	mu          sync.RWMutex
	workers     []*Worker
	workerIndex atomic.Uint32

	// API Key validation
	apiKeys map[string]bool
}

// PromptRequest is the REST API request body
type PromptRequest struct {
	Query        string  `json:"query"`
	Model        string  `json:"model,omitempty"`
	MaxTokens    int32   `json:"max_tokens,omitempty"`
	Temperature  float32 `json:"temperature,omitempty"`
	SystemPrompt string  `json:"system_prompt,omitempty"`
}

// PromptResponse is the REST API response body
type PromptResponse struct {
	RequestID string `json:"request_id"`
	Response  string `json:"response"`
	Model     string `json:"model"`
	Tokens    int32  `json:"tokens"`
	LatencyMs int64  `json:"latency_ms"`
	WorkerID  string `json:"worker_id"`
}

// ErrorResponse represents an API error
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

// NewGateway creates a new gateway instance
func NewGateway(log *logger.Logger, workerAddresses []string, apiKeys []string) (*Gateway, error) {
	m := metrics.NewGatewayMetrics("neurogate_gateway")
	h := health.NewChecker(version)

	// Parse API keys into a map for O(1) lookup
	keyMap := make(map[string]bool)
	for _, key := range apiKeys {
		if key != "" {
			keyMap[key] = true
		}
	}

	g := &Gateway{
		log:           log,
		metrics:       m,
		healthChecker: h,
		workers:       make([]*Worker, 0),
		apiKeys:       keyMap,
	}

	// Initialize workers
	for i, addr := range workerAddresses {
		worker, err := g.createWorker(fmt.Sprintf("worker-%d", i), addr)
		if err != nil {
			log.Warn("failed to connect to worker", "addr", addr, "error", err)
			continue
		}
		g.workers = append(g.workers, worker)
		log.Info("connected to worker", "id", worker.ID, "addr", addr)
	}

	if len(g.workers) == 0 {
		return nil, fmt.Errorf("no workers available")
	}

	// Register health check
	h.Register("workers", func(ctx context.Context) *health.Check {
		healthy := 0
		for _, w := range g.workers {
			if w.Healthy.Load() {
				healthy++
			}
		}

		if healthy == 0 {
			return &health.Check{
				Name:    "workers",
				Status:  health.StatusUnhealthy,
				Message: "no healthy workers",
			}
		}

		if healthy < len(g.workers) {
			return &health.Check{
				Name:    "workers",
				Status:  health.StatusDegraded,
				Message: fmt.Sprintf("%d/%d workers healthy", healthy, len(g.workers)),
			}
		}

		return &health.Check{
			Name:   "workers",
			Status: health.StatusHealthy,
		}
	})

	// Start background health checker
	go g.runHealthChecker()

	return g, nil
}

// createWorker creates and connects to a worker
func (g *Gateway) createWorker(id, addr string) (*Worker, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	worker := &Worker{
		ID:      id,
		Address: addr,
		Conn:    conn,
		Client:  llmv1.NewLLMServiceClient(conn),
		CB: circuitbreaker.New(circuitbreaker.Config{
			Name:             id,
			FailureThreshold: 3,
			SuccessThreshold: 1,
			Timeout:          30 * time.Second,
			OnStateChange: func(name string, from, to circuitbreaker.State) {
				g.log.Info("circuit breaker state change",
					"worker", name,
					"from", from.String(),
					"to", to.String(),
				)
				g.metrics.SetCircuitBreakerState(name, int(to))
			},
		}),
	}
	worker.Healthy.Store(true)

	return worker, nil
}

// runHealthChecker periodically checks worker health
func (g *Gateway) runHealthChecker() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		g.checkWorkersHealth()
	}
}

// checkWorkersHealth checks the health of all workers
func (g *Gateway) checkWorkersHealth() {
	g.mu.RLock()
	workers := g.workers
	g.mu.RUnlock()

	for _, w := range workers {
		go func(worker *Worker) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := worker.Client.HealthCheck(ctx, &llmv1.HealthCheckRequest{
				Timestamp: time.Now().UnixMilli(),
			})

			if err != nil {
				worker.Healthy.Store(false)
				g.log.Debug("worker health check failed", "worker", worker.ID, "error", err)
				return
			}

			worker.Healthy.Store(resp.Healthy)
		}(w)
	}
}

// selectWorker implements Round Robin load balancing
func (g *Gateway) selectWorker() (*Worker, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.workers) == 0 {
		return nil, fmt.Errorf("no workers available")
	}

	// Round Robin selection
	// Try each worker starting from current index
	startIndex := g.workerIndex.Add(1) - 1
	workerCount := uint32(len(g.workers))

	for i := uint32(0); i < workerCount; i++ {
		idx := (startIndex + i) % workerCount
		worker := g.workers[idx]

		// Check if worker is healthy and circuit is not open
		if worker.Healthy.Load() && worker.CB.AllowRequest() {
			return worker, nil
		}
	}

	return nil, fmt.Errorf("all workers are unavailable")
}

// ServeHTTP implements the HTTP handler
func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Route requests
	switch {
	case r.URL.Path == "/prompt" && r.Method == "POST":
		g.handlePrompt(w, r)
	case r.URL.Path == "/health":
		g.healthChecker.HTTPHandler()(w, r)
	case r.URL.Path == "/workers":
		g.handleListWorkers(w, r)
	default:
		g.writeError(w, http.StatusNotFound, "not found", "")
	}
}

// handlePrompt handles the /prompt endpoint
func (g *Gateway) handlePrompt(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	g.metrics.ActiveRequests.Inc()
	defer g.metrics.ActiveRequests.Dec()

	// Validate API key
	if len(g.apiKeys) > 0 {
		authHeader := r.Header.Get("Authorization")
		if !g.validateAPIKey(authHeader) {
			g.writeError(w, http.StatusUnauthorized, "invalid or missing API key", "")
			g.metrics.RecordRequest("POST", "/prompt", "401", time.Since(start).Seconds())
			return
		}
	}

	// Parse request
	var req PromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		g.writeError(w, http.StatusBadRequest, "invalid request body", err.Error())
		g.metrics.RecordRequest("POST", "/prompt", "400", time.Since(start).Seconds())
		return
	}

	if req.Query == "" {
		g.writeError(w, http.StatusBadRequest, "query is required", "")
		g.metrics.RecordRequest("POST", "/prompt", "400", time.Since(start).Seconds())
		return
	}

	// Generate request ID
	requestID := fmt.Sprintf("req-%d", time.Now().UnixNano())
	requestLog := g.log.WithRequestID(requestID)

	// Select a worker
	worker, err := g.selectWorker()
	if err != nil {
		requestLog.Error("no workers available", "error", err)
		g.writeError(w, http.StatusServiceUnavailable, "no workers available", err.Error())
		g.metrics.RecordRequest("POST", "/prompt", "503", time.Since(start).Seconds())
		return
	}

	requestLog.Info("forwarding request to worker",
		"worker_id", worker.ID,
		"query_length", len(req.Query),
	)

	// Forward to worker with circuit breaker
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	var resp *llmv1.PromptResponse
	err = worker.CB.Execute(func() error {
		var callErr error
		resp, callErr = worker.Client.GenerateText(ctx, &llmv1.PromptRequest{
			RequestId:    requestID,
			Prompt:       req.Query,
			Model:        req.Model,
			MaxTokens:    req.MaxTokens,
			Temperature:  req.Temperature,
			SystemPrompt: req.SystemPrompt,
		})
		return callErr
	})

	if err != nil {
		if err == circuitbreaker.ErrCircuitOpen {
			requestLog.Warn("circuit breaker open", "worker", worker.ID)
			g.writeError(w, http.StatusServiceUnavailable, "worker temporarily unavailable", "")
		} else {
			requestLog.Error("worker request failed", "error", err)
			g.writeError(w, http.StatusInternalServerError, "generation failed", err.Error())
		}
		g.metrics.RecordRequest("POST", "/prompt", "500", time.Since(start).Seconds())
		return
	}

	// Build response
	duration := time.Since(start)
	response := PromptResponse{
		RequestID: requestID,
		Response:  resp.Response,
		Model:     resp.Model,
		Tokens:    resp.TotalTokens,
		LatencyMs: duration.Milliseconds(),
		WorkerID:  worker.ID,
	}

	g.metrics.RecordRequest("POST", "/prompt", "200", duration.Seconds())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleListWorkers returns the list of workers and their status
func (g *Gateway) handleListWorkers(w http.ResponseWriter, r *http.Request) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	type workerStatus struct {
		ID      string `json:"id"`
		Address string `json:"address"`
		Healthy bool   `json:"healthy"`
		CBState string `json:"circuit_breaker_state"`
	}

	workers := make([]workerStatus, len(g.workers))
	for i, w := range g.workers {
		workers[i] = workerStatus{
			ID:      w.ID,
			Address: w.Address,
			Healthy: w.Healthy.Load(),
			CBState: w.CB.State().String(),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"workers": workers,
		"count":   len(workers),
	})
}

// validateAPIKey checks if the provided API key is valid
func (g *Gateway) validateAPIKey(authHeader string) bool {
	if authHeader == "" {
		return false
	}

	// Expect "Bearer <token>" format
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return false
	}

	return g.apiKeys[parts[1]]
}

// writeError writes an error response
func (g *Gateway) writeError(w http.ResponseWriter, code int, message, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   message,
		Code:    code,
		Message: detail,
	})
}

func main() {
	// Initialize logger
	log := logger.New(logger.Config{
		Level:   getEnv("LOG_LEVEL", "info"),
		Service: "gateway",
		JSON:    getEnv("LOG_FORMAT", "text") == "json",
	})

	log.Info("starting neurogate gateway",
		"version", version,
		"http_port", getEnv("HTTP_PORT", defaultHTTPPort),
	)

	// Get configuration
	httpPort := getEnv("HTTP_PORT", defaultHTTPPort)
	metricsPort := getEnv("METRICS_PORT", defaultMetricsPort)

	// Parse worker addresses (comma-separated)
	workerAddrs := strings.Split(getEnv("WORKER_ADDRESSES", "localhost:50051"), ",")

	// Parse API keys (comma-separated)
	apiKeys := strings.Split(getEnv("API_KEYS", ""), ",")

	// Create gateway
	gateway, err := NewGateway(log, workerAddrs, apiKeys)
	if err != nil {
		log.Error("failed to create gateway", "error", err)
		os.Exit(1)
	}

	// Create HTTP server for metrics
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsServer := &http.Server{
		Addr:    fmt.Sprintf(":%s", metricsPort),
		Handler: metricsMux,
	}

	go func() {
		log.Info("metrics server started", "addr", metricsServer.Addr)
		if err := metricsServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Error("metrics server error", "error", err)
		}
	}()

	// Create main HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", httpPort),
		Handler:      gateway,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 3 * time.Minute, // Allow for long LLM responses
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info("shutting down gateway...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		server.Shutdown(ctx)
		metricsServer.Shutdown(ctx)
	}()

	log.Info("HTTP server listening", "addr", server.Addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Error("HTTP server error", "error", err)
		os.Exit(1)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
