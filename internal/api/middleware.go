package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/CloudNativeWorks/clustereye-api/internal/logger"
)

// RateLimiter yapısı
type RateLimiter struct {
	visitors map[string]*Visitor
	mu       sync.RWMutex
	rate     int           // requests per minute
	window   time.Duration // time window
}

// Visitor yapısı
type Visitor struct {
	requests  int
	lastReset time.Time
	blocked   bool
	blockTime time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(rate int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*Visitor),
		rate:     rate,
		window:   window,
	}

	// Cleanup goroutine - eski ziyaretçileri temizle
	go rl.cleanup()

	return rl
}

// Allow checks if a request should be allowed
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	visitor, exists := rl.visitors[ip]
	if !exists {
		visitor = &Visitor{
			requests:  1,
			lastReset: time.Now(),
			blocked:   false,
		}
		rl.visitors[ip] = visitor
		return true
	}

	// Eğer ziyaretçi bloklandıysa ve henüz süre dolmadıysa
	if visitor.blocked && time.Since(visitor.blockTime) < 15*time.Minute {
		return false
	}

	// Blok süresi dolmuşsa bloklamayı kaldır
	if visitor.blocked && time.Since(visitor.blockTime) >= 15*time.Minute {
		visitor.blocked = false
		visitor.requests = 0
		visitor.lastReset = time.Now()
	}

	// Zaman penceresi sıfırlandı mı?
	if time.Since(visitor.lastReset) > rl.window {
		visitor.requests = 1
		visitor.lastReset = time.Now()
		return true
	}

	// Rate limit aşıldı mı?
	if visitor.requests >= rl.rate {
		visitor.blocked = true
		visitor.blockTime = time.Now()
		logger.Warn().
			Str("ip", ip).
			Int("requests", visitor.requests).
			Msg("Rate limit exceeded, IP blocked")
		return false
	}

	visitor.requests++
	return true
}

// cleanup removes old visitors
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		for ip, visitor := range rl.visitors {
			// 1 saatten eski ziyaretçileri temizle
			if time.Since(visitor.lastReset) > time.Hour {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(rate int, window time.Duration) gin.HandlerFunc {
	limiter := NewRateLimiter(rate, window)

	return func(c *gin.Context) {
		// Client IP'sini al
		clientIP := c.ClientIP()

		// Rate limit kontrolü
		if !limiter.Allow(clientIP) {
			logger.Warn().
				Str("ip", clientIP).
				Str("path", c.Request.URL.Path).
				Str("method", c.Request.Method).
				Msg("Request blocked due to rate limiting")

			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "Too many requests",
				"message": "Rate limit exceeded. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// SecurityHeadersMiddleware adds security headers
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Security headers
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "default-src 'self'")

		// API specific headers
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")

		c.Next()
	}
}

// RequestLoggingMiddleware logs all requests
func RequestLoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method
		clientIP := c.ClientIP()
		userAgent := c.Request.UserAgent()

		// Process request
		c.Next()

		// Log after request
		duration := time.Since(start)
		statusCode := c.Writer.Status()

		logger.Info().
			Str("method", method).
			Str("path", path).
			Str("ip", clientIP).
			Str("user_agent", userAgent).
			Int("status", statusCode).
			Dur("duration", duration).
			Msg("Request processed")

		// Log suspicious requests
		if statusCode >= 400 {
			logger.Warn().
				Str("method", method).
				Str("path", path).
				Str("ip", clientIP).
				Str("user_agent", userAgent).
				Int("status", statusCode).
				Msg("Suspicious request detected")
		}
	}
}
