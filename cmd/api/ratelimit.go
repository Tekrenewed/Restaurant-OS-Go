package main

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter implements a per-IP sliding window rate limiter.
// It tracks request counts per IP address over a configurable window,
// blocking requests that exceed the threshold.
type RateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	limit    int           // max requests per window
	window   time.Duration // sliding window duration
}

// NewRateLimiter creates a rate limiter that allows `limit` requests per `window` per IP.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}

	// Background cleanup: evict stale entries every 5 minutes to prevent memory leak
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()

	return rl
}

// Allow checks if a request from the given IP should be allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Filter out expired entries
	reqs := rl.requests[ip]
	active := reqs[:0]
	for _, t := range reqs {
		if t.After(cutoff) {
			active = append(active, t)
		}
	}

	if len(active) >= rl.limit {
		rl.requests[ip] = active
		return false
	}

	rl.requests[ip] = append(active, now)
	return true
}

// cleanup removes IPs that have no recent requests
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.window)
	for ip, reqs := range rl.requests {
		active := reqs[:0]
		for _, t := range reqs {
			if t.After(cutoff) {
				active = append(active, t)
			}
		}
		if len(active) == 0 {
			delete(rl.requests, ip)
		} else {
			rl.requests[ip] = active
		}
	}
}

// RateLimitMiddleware wraps an http.HandlerFunc with IP-based rate limiting.
// Returns 429 Too Many Requests when the limit is exceeded.
func RateLimitMiddleware(rl *RateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract client IP — Cloud Run sets X-Forwarded-For
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.RemoteAddr
		}
		// Take only the first IP in the chain (the real client)
		for i, c := range ip {
			if c == ',' {
				ip = ip[:i]
				break
			}
		}

		if !rl.Allow(ip) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"rate_limited","message":"Too many requests. Please try again later."}`, http.StatusTooManyRequests)
			return
		}

		next(w, r)
	}
}
