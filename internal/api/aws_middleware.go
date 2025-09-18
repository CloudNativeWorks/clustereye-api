package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/CloudNativeWorks/clustereye-api/internal/logger"
)

// AWSRateLimiter, AWS API'leri için rate limiting yapısı
type AWSRateLimiter struct {
	mu       sync.RWMutex
	requests map[string][]time.Time
	limit    int
	window   time.Duration
}

// NewAWSRateLimiter, yeni bir AWS rate limiter oluşturur
func NewAWSRateLimiter(limit int, window time.Duration) *AWSRateLimiter {
	return &AWSRateLimiter{
		requests: make(map[string][]time.Time),
		limit:    limit,
		window:   window,
	}
}

// IsAllowed, belirli bir IP için isteğin izinli olup olmadığını kontrol eder
func (rl *AWSRateLimiter) IsAllowed(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Eski kayıtları temizle
	if requests, exists := rl.requests[ip]; exists {
		validRequests := make([]time.Time, 0)
		for _, reqTime := range requests {
			if reqTime.After(cutoff) {
				validRequests = append(validRequests, reqTime)
			}
		}
		rl.requests[ip] = validRequests
	}

	// Mevcut istek sayısını kontrol et
	if len(rl.requests[ip]) >= rl.limit {
		return false
	}

	// Yeni isteği kaydet
	rl.requests[ip] = append(rl.requests[ip], now)
	return true
}

// AWSRateLimitMiddleware, AWS endpoint'leri için rate limiting middleware'i
func AWSRateLimitMiddleware() gin.HandlerFunc {
	// AWS API'leri için dakikada 10 istek limiti
	limiter := NewAWSRateLimiter(10, time.Minute)

	return func(c *gin.Context) {
		clientIP := c.ClientIP()

		if !limiter.IsAllowed(clientIP) {
			logger.Warn().
				Str("ip", clientIP).
				Str("path", c.Request.URL.Path).
				Msg("AWS API rate limit exceeded")

			c.JSON(http.StatusTooManyRequests, gin.H{
				"status": "error",
				"error":  "Rate limit exceeded. Please try again later.",
				"code":   "AWS_RATE_LIMIT_EXCEEDED",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// AWSSecurityMiddleware, AWS endpoint'leri için güvenlik middleware'i
func AWSSecurityMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Request body boyutunu kontrol et (max 1MB)
		if c.Request.ContentLength > 1024*1024 {
			logger.Warn().
				Int64("content_length", c.Request.ContentLength).
				Str("ip", c.ClientIP()).
				Msg("AWS API request body too large")

			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"status": "error",
				"error":  "Request body too large",
				"code":   "REQUEST_TOO_LARGE",
			})
			c.Abort()
			return
		}

		// Content-Type kontrolü
		contentType := c.GetHeader("Content-Type")
		if contentType != "application/json" {
			logger.Warn().
				Str("content_type", contentType).
				Str("ip", c.ClientIP()).
				Msg("AWS API invalid content type")

			c.JSON(http.StatusUnsupportedMediaType, gin.H{
				"status": "error",
				"error":  "Content-Type must be application/json",
				"code":   "INVALID_CONTENT_TYPE",
			})
			c.Abort()
			return
		}

		// Güvenlik başlıklarını ekle
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")

		c.Next()
	}
}
