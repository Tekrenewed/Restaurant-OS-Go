package middleware

import (
	"context"
	"log"
	"net/http"
	"runtime/debug"
	"time"
)

// Recovery catches panics in HTTP handlers and returns a 500 instead of crashing the server.
// Without this, a single nil-pointer in any handler kills the entire Go process.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[PANIC] %s %s: %v\n%s", r.Method, r.URL.Path, err, debug.Stack())
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"internal_error","message":"An unexpected error occurred"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Timeout adds a deadline to every request context, preventing runaway queries
// or slow external calls from holding connections open indefinitely.
func Timeout(duration time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequestLogger provides structured access logging for every request.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(wrapped, r)
		log.Printf("[%s] %s %s → %d (%s)",
			r.Method, r.URL.Path, r.RemoteAddr, wrapped.status, time.Since(start).Round(time.Millisecond))
	})
}

// statusWriter wraps http.ResponseWriter to capture the status code for logging.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
