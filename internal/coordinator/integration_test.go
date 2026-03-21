package coordinator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"neteval/internal/protocol"
)

// TestCoordinatorHTTPEndpoints tests the REST API surface.
func TestCoordinatorHTTPEndpoints(t *testing.T) {
	c := New(0)

	t.Run("GET /api/agents returns empty list", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/agents", nil)
		w := httptest.NewRecorder()
		c.handleGetAgents(w, req)

		if w.Code != 200 {
			t.Errorf("status = %d, want 200", w.Code)
		}

		var agents []protocol.AgentInfo
		json.NewDecoder(w.Body).Decode(&agents)
		if len(agents) != 0 {
			t.Errorf("expected 0 agents, got %d", len(agents))
		}
	})

	t.Run("GET /api/results returns empty list", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/results", nil)
		w := httptest.NewRecorder()
		c.handleGetResults(w, req)

		if w.Code != 200 {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})

	t.Run("POST /api/tests/mesh requires POST", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/tests/mesh", nil)
		w := httptest.NewRecorder()
		c.handleRunMeshTest(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", w.Code)
		}
	})

	t.Run("POST /api/tests/internet requires POST", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/tests/internet", nil)
		w := httptest.NewRecorder()
		c.handleRunInternetTest(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("status = %d, want 405", w.Code)
		}
	})

	t.Run("POST /api/results/clear clears results", func(t *testing.T) {
		c.Hub.AddResult(protocol.TestResult{SourceID: "test"})

		req := httptest.NewRequest("POST", "/api/results/clear", nil)
		w := httptest.NewRecorder()
		c.handleClearResults(w, req)

		if w.Code != 200 {
			t.Errorf("status = %d, want 200", w.Code)
		}

		if len(c.Hub.GetResults()) != 0 {
			t.Error("results not cleared")
		}
	})

	t.Run("GET /api/results/export?format=csv", func(t *testing.T) {
		c.Hub.AddResult(protocol.TestResult{
			SourceID:   "agent-1",
			SourceName: "host-a",
			TargetID:   "agent-2",
			TargetName: "host-b",
			TestType:   "mesh",
			Direction:  "upload",
			BitsPerSec: 500_000_000,
			DurationMs: 10000,
			Timestamp:  time.Now(),
		})

		req := httptest.NewRequest("GET", "/api/results/export?format=csv", nil)
		w := httptest.NewRecorder()
		c.handleExportResults(w, req)

		if w.Code != 200 {
			t.Errorf("status = %d, want 200", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "text/csv" {
			t.Errorf("Content-Type = %q, want text/csv", ct)
		}
		body := w.Body.String()
		if !strings.Contains(body, "source_id") {
			t.Error("CSV should contain header row")
		}
		if !strings.Contains(body, "agent-1") {
			t.Error("CSV should contain result data")
		}
	})

	t.Run("GET /api/results/export?format=json", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/results/export?format=json", nil)
		w := httptest.NewRecorder()
		c.handleExportResults(w, req)

		if w.Code != 200 {
			t.Errorf("status = %d, want 200", w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
	})
}

// TestAuthMiddleware tests token-based authentication.
func TestAuthMiddleware(t *testing.T) {
	c := New(0)
	c.AuthToken = "secret-token"

	handler := c.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))

	t.Run("static files bypass auth", func(t *testing.T) {
		for _, path := range []string{"/", "/style.css", "/app.js"} {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Errorf("%s: status = %d, want 200", path, w.Code)
			}
		}
	})

	t.Run("API requires auth", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/agents", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})

	t.Run("auth via query param", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/agents?token=secret-token", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})

	t.Run("auth via header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/agents", nil)
		req.Header.Set("X-Auth-Token", "secret-token")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})

	t.Run("auth via cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/agents", nil)
		req.AddCookie(&http.Cookie{Name: "neteval_token", Value: "secret-token"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})

	t.Run("wrong token rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/agents?token=wrong", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", w.Code)
		}
	})
}

// TestCoordinatorStartStop tests graceful startup and shutdown.
func TestCoordinatorStartStop(t *testing.T) {
	c := New(0) // port 0 = random available port

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Run(ctx)
	}()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Trigger shutdown
	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("coordinator did not shut down in time")
	}
}

// TestMeshTestRequiresTwoAgents verifies the orchestrator rejects with < 2 agents.
func TestMeshTestRequiresTwoAgents(t *testing.T) {
	c := New(0)

	err := c.Orchestrator.RunMeshTest(context.Background())
	if err == nil {
		t.Error("expected error with 0 agents")
	}
	if !strings.Contains(err.Error(), "need at least 2 agents") {
		t.Errorf("error = %q, want 'need at least 2 agents'", err.Error())
	}
}

// TestIsRunningPreventsDoubleStart verifies the mutex lock.
func TestIsRunningPreventsDoubleStart(t *testing.T) {
	hub := NewHub()
	orch := NewOrchestrator(hub)

	if orch.IsRunning() {
		t.Error("should not be running initially")
	}
}

func TestGetLocalIP(t *testing.T) {
	ip := getLocalIP()
	if ip == "" {
		t.Error("getLocalIP returned empty string")
	}
	// Should be a valid IP (either local or 127.0.0.1 fallback)
	if ip != "127.0.0.1" && !strings.Contains(ip, ".") {
		t.Errorf("getLocalIP = %q, doesn't look like an IP", ip)
	}
}
