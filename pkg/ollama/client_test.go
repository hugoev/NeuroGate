package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient_DefaultURL(t *testing.T) {
	client := NewClient("")

	if client.baseURL != "http://localhost:11434" {
		t.Errorf("expected default URL, got %s", client.baseURL)
	}
}

func TestNewClient_CustomURL(t *testing.T) {
	client := NewClient("http://custom:1234")

	if client.baseURL != "http://custom:1234" {
		t.Errorf("expected custom URL, got %s", client.baseURL)
	}
}

func TestClient_Ping_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ModelsResponse{})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	err := client.Ping(context.Background())

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestClient_Ping_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	err := client.Ping(context.Background())

	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestClient_Generate_Success(t *testing.T) {
	expectedResponse := &GenerateResponse{
		Model:     "llama3.2",
		Response:  "Hello, world!",
		Done:      true,
		EvalCount: 10,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/generate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var req GenerateRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.Model != "llama3.2" {
			t.Errorf("expected model llama3.2, got %s", req.Model)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(expectedResponse)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.Generate(context.Background(), &GenerateRequest{
		Model:  "llama3.2",
		Prompt: "Say hello",
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.Response != expectedResponse.Response {
		t.Errorf("expected response %q, got %q", expectedResponse.Response, resp.Response)
	}
}

func TestClient_Generate_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("model not found"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.Generate(context.Background(), &GenerateRequest{
		Model:  "nonexistent",
		Prompt: "Hello",
	})

	if err == nil {
		t.Error("expected error for bad request")
	}
}

func TestClient_ListModels(t *testing.T) {
	expectedModels := []Model{
		{Name: "llama3.2", Size: 1000000},
		{Name: "mistral", Size: 2000000},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ModelsResponse{Models: expectedModels})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	models, err := client.ListModels(context.Background())

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(models) != len(expectedModels) {
		t.Errorf("expected %d models, got %d", len(expectedModels), len(models))
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := client.Ping(ctx)

	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
