package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter is a Redis-based sliding window rate limiter middleware.
type RateLimiter struct {
	Redis *redis.Client
}

// NewRateLimiter creates a rate limiter. If redis is nil, all requests are allowed.
func NewRateLimiter(rdb *redis.Client) *RateLimiter {
	return &RateLimiter{Redis: rdb}
}

// Limit returns middleware that limits requests per key.
// keyFunc extracts the rate limit key from the request (e.g. IP, username).
// maxRequests is the maximum number of requests allowed in the window.
// window is the time window duration.
func (rl *RateLimiter) Limit(keyFunc func(r *http.Request) string, maxRequests int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rl.Redis == nil {
				next.ServeHTTP(w, r)
				return
			}

			key := keyFunc(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			rlKey := fmt.Sprintf("ratelimit:%s", key)
			ctx, cancel := context.WithTimeout(r.Context(), 500*time.Millisecond)
			defer cancel()

			// Sliding window: count requests in current window
			now := time.Now().UnixMilli()
			windowStart := now - window.Milliseconds()

			pipe := rl.Redis.Pipeline()
			pipe.ZRemRangeByScore(ctx, rlKey, "0", fmt.Sprintf("%d", windowStart))
			countCmd := pipe.ZCard(ctx, rlKey)
			pipe.ZAdd(ctx, rlKey, redis.Z{Score: float64(now), Member: now})
			pipe.Expire(ctx, rlKey, window)
			if _, err := pipe.Exec(ctx); err != nil {
				slog.Warn("rate limiter redis error", "error", err)
				next.ServeHTTP(w, r)
				return
			}

			if countCmd.Val() >= int64(maxRequests) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", fmt.Sprintf("%d", int(window.Seconds())))
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"ok":false,"msg":"rate limit exceeded, try again later"}`)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// IPKey extracts the client IP from the request.
func IPKey(r *http.Request) string {
	// Check X-Forwarded-For (set by reverse proxy)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// LoginKey extracts IP + username for login rate limiting.
func LoginKey(r *http.Request) string {
	ip := IPKey(r)
	return fmt.Sprintf("login:%s", ip)
}

// CORSMiddleware returns a middleware that handles CORS headers.
// allowedOrigins is a comma-separated list of allowed origins. Use "*" to allow all.
func CORSMiddleware(allowedOrigins string) func(http.Handler) http.Handler {
	origins := make(map[string]bool)
	for _, o := range strings.Split(allowedOrigins, ",") {
		origins[strings.TrimSpace(o)] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if allowedOrigins == "*" || origins[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
