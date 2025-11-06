package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestServer wraps httptest.Server with request capture for testing
type TestServer struct {
	httpServer *httptest.Server
	handler    *Handler
	capture    *RequestCapture
	t          *testing.T
}

// CapturedRequest stores details about a request received by the mock server
type CapturedRequest struct {
	Method    string
	Path      string
	Headers   http.Header
	Body      []byte
	Timestamp time.Time
}

// RequestCapture collects all requests for inspection
type RequestCapture struct {
	mu       sync.RWMutex
	requests []CapturedRequest
	skipMeta bool
	metaLeft int
	logger   *slog.Logger
}

// NewRequestCapture creates a new request capture collector
func NewRequestCapture(logger *slog.Logger) *RequestCapture {
	if logger == nil {
		logger = slog.Default()
	}

	return &RequestCapture{
		requests: make([]CapturedRequest, 0),
		logger:   logger,
	}
}

// Wrap wraps an http.Handler to capture requests before passing them through
func (rc *RequestCapture) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		// Restore body for the actual handler
		r.Body = io.NopCloser(bytes.NewBuffer(body))

		shouldCapture := true
		if rc.skipMeta && r.Method == http.MethodPost && r.URL.Path == "/info" {
			var payload struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(body, &payload); err == nil {
				if payload.Type == "meta" || payload.Type == "spotMeta" {
					shouldCapture = false
					rc.metaLeft--
					if rc.metaLeft <= 0 {
						rc.skipMeta = false
					}
				} else {
					rc.skipMeta = false
				}
			} else {
				rc.skipMeta = false
			}
		}

		if rc.logger != nil {
			rc.logger.Debug("request received", "method", r.Method, "path", r.URL.Path, "body", string(body))
		}

		if shouldCapture {
			rc.mu.Lock()
			rc.requests = append(rc.requests, CapturedRequest{
				Method:    r.Method,
				Path:      r.URL.Path,
				Headers:   r.Header.Clone(),
				Body:      append([]byte(nil), body...), // Deep copy
				Timestamp: time.Now(),
			})
			rc.mu.Unlock()
		}

		// Pass to actual handler
		lrw := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(lrw, r)

		if rc.logger != nil {
			rc.logger.Debug("response sent", "method", r.Method, "path", r.URL.Path, "status", lrw.status(), "body", lrw.body())
		}
	})
}

type responseRecorder struct {
	http.ResponseWriter
	buf  bytes.Buffer
	code int
}

func (rr *responseRecorder) WriteHeader(statusCode int) {
	rr.code = statusCode
	rr.ResponseWriter.WriteHeader(statusCode)
}

func (rr *responseRecorder) Write(data []byte) (int, error) {
	if rr.code == 0 {
		rr.code = http.StatusOK
	}
	rr.buf.Write(data)
	return rr.ResponseWriter.Write(data)
}

func (rr *responseRecorder) status() int {
	if rr.code == 0 {
		return http.StatusOK
	}
	return rr.code
}

func (rr *responseRecorder) body() string {
	return rr.buf.String()
}

// GetRequests returns a copy of all captured requests
func (rc *RequestCapture) GetRequests() []CapturedRequest {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	// Return a copy to avoid race conditions
	result := make([]CapturedRequest, len(rc.requests))
	copy(result, rc.requests)
	return result
}

// Count returns the number of captured requests
func (rc *RequestCapture) Count() int {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return len(rc.requests)
}

// Clear removes all captured requests
func (rc *RequestCapture) Clear() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.requests = rc.requests[:0]
	rc.skipMeta = true
	rc.metaLeft = 2
}

type TestServerOption = Option

// NewTestServer creates a new test server with automatic cleanup
// Each test gets an isolated server instance on a random port
func NewTestServer(t *testing.T, opts ...TestServerOption) *TestServer {
	cfg := options{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	logger := cfg.logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	handler := NewHandler(WithLogger(logger))
	capture := NewRequestCapture(logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/exchange", handler.HandleExchange)
	mux.HandleFunc("/info", handler.HandleInfo)
	mux.HandleFunc("/health", handler.HandleHealth)

	// Wrap with capture middleware
	capturedMux := capture.Wrap(mux)

	// Start httptest server on random port
	httpServer := httptest.NewServer(capturedMux)

	ts := &TestServer{
		httpServer: httpServer,
		handler:    handler,
		capture:    capture,
		t:          t,
	}

	// Automatic cleanup when test finishes
	t.Cleanup(func() {
		ts.Close()
	})

	return ts
}

// URL returns the base URL of the test server (e.g., "http://127.0.0.1:12345")
func (ts *TestServer) URL() string {
	return ts.httpServer.URL
}

// Close shuts down the test server and blocks until all requests complete
func (ts *TestServer) Close() {
	if ts.httpServer != nil {
		ts.httpServer.Close()
	}
}

// GetRequests returns all captured requests
func (ts *TestServer) GetRequests() []CapturedRequest {
	return ts.capture.GetRequests()
}

// GetExchangeRequests returns all decoded /exchange requests
func (ts *TestServer) GetExchangeRequests() []ExchangeRequest {
	requests := ts.capture.GetRequests()
	var result []ExchangeRequest

	for _, req := range requests {
		if req.Path == "/exchange" && req.Method == http.MethodPost {
			var exchangeReq ExchangeRequest
			if err := json.Unmarshal(req.Body, &exchangeReq); err == nil {
				result = append(result, exchangeReq)
			}
		}
	}

	return result
}

// GetInfoRequests returns all decoded /info requests
func (ts *TestServer) GetInfoRequests() []InfoRequest {
	requests := ts.capture.GetRequests()
	var result []InfoRequest

	for _, req := range requests {
		if req.Path == "/info" && req.Method == http.MethodPost {
			var infoReq InfoRequest
			if err := json.Unmarshal(req.Body, &infoReq); err == nil {
				result = append(result, infoReq)
			}
		}
	}

	return result
}

// RequestCount returns the total number of requests received
func (ts *TestServer) RequestCount() int {
	return ts.capture.Count()
}

// ClearRequests removes all captured request history
func (ts *TestServer) ClearRequests() {
	ts.capture.Clear()
}

// State returns the server's internal order state for inspection/manipulation
func (ts *TestServer) State() *State {
	return ts.handler.state
}

// GetOrder returns a stored order by CLOID for assertions
func (ts *TestServer) GetOrder(cloid string) (*OrderDetail, bool) {
	return ts.handler.state.GetOrder(cloid)
}

// GetOrderByOid returns a stored order by OID for assertions
func (ts *TestServer) GetOrderByOid(oid int64) (*OrderDetail, bool) {
	return ts.handler.state.GetOrderByOid(oid)
}
