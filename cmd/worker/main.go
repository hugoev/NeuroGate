// Worker Service - gRPC server for LLM inference
// This service connects to Ollama and handles inference requests from the Gateway
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	llmv1 "github.com/hugovillarreal/neurogate/api/proto/llm/v1"
	"github.com/hugovillarreal/neurogate/pkg/health"
	"github.com/hugovillarreal/neurogate/pkg/logger"
	"github.com/hugovillarreal/neurogate/pkg/metrics"
	"github.com/hugovillarreal/neurogate/pkg/ollama"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

const (
	defaultGRPCPort    = "50051"
	defaultMetricsPort = "9090"
	defaultOllamaURL   = "http://localhost:11434"
	defaultModel       = "llama3.2"
	version            = "1.0.0"
)

// WorkerServer implements the LLMService gRPC interface
type WorkerServer struct {
	llmv1.UnimplementedLLMServiceServer

	log           *logger.Logger
	ollamaClient  *ollama.Client
	metrics       *metrics.Metrics
	healthChecker *health.Checker

	// State tracking
	activeRequests atomic.Int32
	mu             sync.RWMutex
	ollamaHealthy  atomic.Bool
}

// NewWorkerServer creates a new worker server
func NewWorkerServer(log *logger.Logger, ollamaURL string) *WorkerServer {
	m := metrics.NewWorkerMetrics("neurogate_worker")
	h := health.NewChecker(version)

	server := &WorkerServer{
		log:           log,
		ollamaClient:  ollama.NewClient(ollamaURL),
		metrics:       m,
		healthChecker: h,
	}

	// Register Ollama health check
	h.Register("ollama", func(ctx context.Context) *health.Check {
		start := time.Now()
		err := server.ollamaClient.Ping(ctx)
		latency := time.Since(start)

		if err != nil {
			server.ollamaHealthy.Store(false)
			server.metrics.SetOllamaConnected(false)
			return &health.Check{
				Name:    "ollama",
				Status:  health.StatusUnhealthy,
				Message: err.Error(),
				Latency: latency,
			}
		}

		server.ollamaHealthy.Store(true)
		server.metrics.SetOllamaConnected(true)
		return &health.Check{
			Name:    "ollama",
			Status:  health.StatusHealthy,
			Latency: latency,
		}
	})

	return server
}

// StartHealthChecker starts a background goroutine to check Ollama health
func (s *WorkerServer) StartHealthChecker(ctx context.Context) {
	// Check immediately on startup
	s.checkOllamaHealth()

	// Then check periodically
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkOllamaHealth()
			}
		}
	}()
}

// checkOllamaHealth checks if Ollama is reachable
func (s *WorkerServer) checkOllamaHealth() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := s.ollamaClient.Ping(ctx)
	if err != nil {
		s.ollamaHealthy.Store(false)
		s.metrics.SetOllamaConnected(false)
		s.log.Debug("ollama health check failed", "error", err)
	} else {
		s.ollamaHealthy.Store(true)
		s.metrics.SetOllamaConnected(true)
		s.log.Debug("ollama health check passed")
	}
}

// GenerateText implements the LLMService.GenerateText RPC
func (s *WorkerServer) GenerateText(ctx context.Context, req *llmv1.PromptRequest) (*llmv1.PromptResponse, error) {
	requestLog := s.log.WithRequestID(req.RequestId)
	requestLog.Info("received generate request",
		"model", req.Model,
		"prompt_length", len(req.Prompt),
	)

	// Track active requests
	s.activeRequests.Add(1)
	s.metrics.ActiveInferences.Inc()
	defer func() {
		s.activeRequests.Add(-1)
		s.metrics.ActiveInferences.Dec()
	}()

	// Update worker load metric
	load := float64(s.activeRequests.Load()) / 10.0 // Assuming max 10 concurrent requests
	if load > 1.0 {
		load = 1.0
	}
	s.metrics.WorkerLoad.Set(load)

	// Validate request
	if req.Prompt == "" {
		return nil, status.Error(codes.InvalidArgument, "prompt is required")
	}

	model := req.Model
	if model == "" {
		model = defaultModel
	}

	// Build Ollama request
	ollamaReq := &ollama.GenerateRequest{
		Model:  model,
		Prompt: req.Prompt,
		System: req.SystemPrompt,
		Options: &ollama.GenerateOptions{
			Temperature: float64(req.Temperature),
			NumPredict:  int(req.MaxTokens),
		},
	}

	// Call Ollama
	start := time.Now()
	resp, err := s.ollamaClient.Generate(ctx, ollamaReq)
	duration := time.Since(start)

	if err != nil {
		requestLog.Error("ollama generation failed", "error", err)
		s.metrics.OllamaRequestErrors.WithLabelValues(model, "generation_error").Inc()
		return nil, status.Errorf(codes.Internal, "failed to generate text: %v", err)
	}

	// Record metrics
	inferenceSeconds := duration.Seconds()
	tokensGenerated := resp.EvalCount
	s.metrics.RecordInference(model, inferenceSeconds, tokensGenerated)
	s.metrics.OllamaRequestsTotal.WithLabelValues(model, "success").Inc()

	requestLog.Info("generation complete",
		"duration_ms", duration.Milliseconds(),
		"tokens_generated", tokensGenerated,
	)

	return &llmv1.PromptResponse{
		RequestId:        req.RequestId,
		Response:         resp.Response,
		PromptTokens:     int32(resp.PromptEvalCount),
		CompletionTokens: int32(resp.EvalCount),
		TotalTokens:      int32(resp.PromptEvalCount + resp.EvalCount),
		InferenceTimeMs:  duration.Milliseconds(),
		Model:            model,
	}, nil
}

// StreamGenerateText implements streaming text generation
func (s *WorkerServer) StreamGenerateText(req *llmv1.PromptRequest, stream grpc.ServerStreamingServer[llmv1.TokenResponse]) error {
	// For now, we'll implement non-streaming and send in one chunk
	// Full streaming implementation would require changes to the Ollama client

	resp, err := s.GenerateText(stream.Context(), req)
	if err != nil {
		return err
	}

	// Send the response as a single token
	return stream.Send(&llmv1.TokenResponse{
		RequestId:       req.RequestId,
		Token:           resp.Response,
		Done:            true,
		TokensGenerated: resp.CompletionTokens,
	})
}

// HealthCheck implements the health check RPC
func (s *WorkerServer) HealthCheck(ctx context.Context, req *llmv1.HealthCheckRequest) (*llmv1.HealthCheckResponse, error) {
	activeReqs := s.activeRequests.Load()
	load := float64(activeReqs) / 10.0
	if load > 1.0 {
		load = 1.0
	}

	return &llmv1.HealthCheckResponse{
		Healthy:         s.ollamaHealthy.Load(),
		Load:            float32(load),
		ActiveRequests:  activeReqs,
		Version:         version,
		OllamaConnected: s.ollamaHealthy.Load(),
	}, nil
}

// startMetricsServer starts the HTTP server for Prometheus metrics
func startMetricsServer(addr string, health *health.Checker) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/health", health.HTTPHandler())
	mux.HandleFunc("/ready", health.HTTPHandler())

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			fmt.Printf("metrics server error: %v\n", err)
		}
	}()

	return server
}

func main() {
	// Initialize logger
	log := logger.New(logger.Config{
		Level:   getEnv("LOG_LEVEL", "info"),
		Service: "worker",
		JSON:    getEnv("LOG_FORMAT", "text") == "json",
	})

	log.Info("starting neurogate worker",
		"version", version,
		"grpc_port", getEnv("GRPC_PORT", defaultGRPCPort),
	)

	// Get configuration from environment
	grpcPort := getEnv("GRPC_PORT", defaultGRPCPort)
	metricsPort := getEnv("METRICS_PORT", defaultMetricsPort)
	ollamaURL := getEnv("OLLAMA_URL", defaultOllamaURL)

	// Create worker server
	server := NewWorkerServer(log, ollamaURL)

	// Start background health checker for Ollama
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server.StartHealthChecker(ctx)

	// Start metrics/health server
	metricsAddr := fmt.Sprintf(":%s", metricsPort)
	metricsServer := startMetricsServer(metricsAddr, server.healthChecker)
	log.Info("metrics server started", "addr", metricsAddr)

	// Create gRPC server
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(unaryLoggingInterceptor(log)),
	)
	llmv1.RegisterLLMServiceServer(grpcServer, server)
	reflection.Register(grpcServer) // Enable reflection for debugging

	// Start gRPC server
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", grpcPort))
	if err != nil {
		log.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Info("shutting down worker...")

		grpcServer.GracefulStop()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		metricsServer.Shutdown(ctx)
	}()

	log.Info("gRPC server listening", "addr", listener.Addr())
	if err := grpcServer.Serve(listener); err != nil {
		log.Error("gRPC server error", "error", err)
		os.Exit(1)
	}
}

// unaryLoggingInterceptor logs gRPC requests
func unaryLoggingInterceptor(log *logger.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		log.Info("grpc request",
			"method", info.FullMethod,
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)

		return resp, err
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
