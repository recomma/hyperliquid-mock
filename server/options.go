package server

import "log/slog"

type options struct {
	logger *slog.Logger
}

// Option configures behaviour for handlers and test servers.
type Option func(*options)

// WithLogger injects a slog.Logger used for request/response logging.
func WithLogger(logger *slog.Logger) Option {
	return func(opts *options) {
		opts.logger = logger
	}
}
