package api

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// loginRateLimiter throttles /api/admin/login per client IP to slow down
// brute-force password guessing (see docs/PLAN.md rate-limiting decision:
// only the login endpoint needs this — /api/register relies on the DB's
// partial unique index instead).
type loginRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{limiters: make(map[string]*rate.Limiter)}
}

func (l *loginRateLimiter) limiterFor(key string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	lim, ok := l.limiters[key]
	if !ok {
		// 1 request/sec sustained, burst of 5 — enough for normal retry of a
		// mistyped password, not enough for rapid guessing.
		lim = rate.NewLimiter(1, 5)
		l.limiters[key] = lim
	}
	return lim
}

func (l *loginRateLimiter) middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.limiterFor(c.ClientIP()).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many login attempts, slow down"})
			return
		}
		c.Next()
	}
}
