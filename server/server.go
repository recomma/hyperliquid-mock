package server

import (
	"log/slog"
	"net/http"
	"os"
)

// Run starts the HTTP server
func Run(addr string) error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	handler := NewHandler(WithLogger(logger))

	mux := http.NewServeMux()
	mux.HandleFunc("/exchange", handler.HandleExchange)
	mux.HandleFunc("/info", handler.HandleInfo)
	mux.HandleFunc("/health", handler.HandleHealth)
	mux.HandleFunc("/ws", handler.wsm.HandleConnection)

	// Log all requests
	loggedMux := loggingMiddleware(logger, mux)

	logger.Info("Mock Hyperliquid API server listening", "addr", addr)
	logger.Info("Endpoints",
		"exchange", addr+"/exchange",
		"info", addr+"/info",
		"health", addr+"/health",
		"websocket", "ws://"+addr[7:]+"/ws")

	return http.ListenAndServe(addr, loggedMux)
}

// loggingMiddleware logs all incoming requests
func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if logger == nil {
			logger = slog.Default()
		}
		logger.Info("incoming request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}
