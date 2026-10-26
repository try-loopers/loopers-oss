package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/try-loopers/loopers-oss/internal/keyring"
	"github.com/try-loopers/loopers-oss/internal/logging"
	"github.com/try-loopers/loopers-oss/internal/proxy"
)

const (
	// Context keys
	RequestIDKey   = "RequestID"
	RequestBodyCtx = "RequestBody"
	JWTClaimsCtx   = "JWTClaims"
)

// RequestID injects X-Request-ID into context and response headers.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" {
			reqID = uuid.New().String()
		} else {
			// Sanitize to prevent log injection (alphanumeric and hyphens only, max 64 chars)
			if len(reqID) > 64 {
				reqID = reqID[:64]
			}
			reqID = strings.Map(func(r rune) rune {
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
					return r
				}
				return -1
			}, reqID)
			if reqID == "" {
				reqID = uuid.New().String()
			}
		}
		c.Set(RequestIDKey, reqID)
		c.Header("X-Request-ID", reqID)
		c.Next()
	}
}

// Recovery handles panics and returns an OpenAI-compatible 500 error, never crashing the server.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				var reqID string
				if val, exists := c.Get(RequestIDKey); exists {
					reqID, _ = val.(string)
				}
				logging.Logger.Error().
					Interface("panic", err).
					Str("stack", string(debug.Stack())).
					Str("request_id", reqID).
					Msg("panic recovered")

				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{
						"message": "An internal error occurred. Check server logs.",
						"type":    "internal_server_error",
						"code":    "internal_error",
					},
				})
			}
		}()
		c.Next()
	}
}

// RequestLogger logs HTTP request metadata.
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		var reqID string
		if val, exists := c.Get(RequestIDKey); exists {
			reqID, _ = val.(string)
		}

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		clientIP := c.ClientIP()

		logging.Logger.Info().
			Str("request_id", reqID).
			Str("client_ip", clientIP).
			Str("method", c.Request.Method).
			Str("path", path).
			Str("query", raw).
			Int("status", status).
			Dur("latency", latency).
			Msg("HTTP request processed")
	}
}

// MaxBytesReader limits the request body size to prevent OOM.
func MaxBytesReader(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// KeyExtractor extracts X-Loopers-Provider-Key and stores it in context, stripping it from headers.
// It also extracts the Authorization header containing the Loopers proxy key or JWT.
func KeyExtractor(jwks *keyring.JWKSValidator) gin.HandlerFunc {
	return func(c *gin.Context) {
		providerKey := c.GetHeader("X-Loopers-Provider-Key")
		c.Request.Header.Del("X-Loopers-Provider-Key")

		authHeader := c.GetHeader("Authorization")
		var proxyKey string
		if strings.HasPrefix(authHeader, "Bearer ") {
			proxyKey = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			proxyKey = authHeader
		}

		if providerKey != "" {
			c.Set(proxy.ProviderKeyCtx, providerKey)
		}

		if proxyKey != "" {
			c.Set("ProxyKey", proxyKey)
			// A valid JWT has exactly two dots (Header.Payload.Signature)
			if jwks != nil && strings.Count(proxyKey, ".") == 2 {
				meta, err := jwks.ValidateJWT(c.Request.Context(), proxyKey)
				if err == nil {
					c.Set(JWTClaimsCtx, meta)
				} else {
					logging.Logger.Warn().Err(err).Msg("JWT validation failed in KeyExtractor")
					c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid JWT token"})
					return
				}
			} else {
				if viper.GetBool("zsp.jwt_only") {
					logging.Logger.Warn().Msg("Rejected non-JWT token because zsp.jwt_only is true")
					c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Only JWT tokens are allowed"})
					return
				}
				if !keyring.ValidateLoopersKey(proxyKey) {
					c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid loopers key format"})
					return
				}
				c.Set("KeyHash", keyring.HashKey(proxyKey))
			}
		}
		c.Next()
	}
}

// bufferPool reuses bytes.Buffer to dramatically reduce GC pressure during request body buffering.
var bufferPool = sync.Pool{
	New: func() interface{} {
		// Pre-allocate a 4KB buffer
		buf := bytes.NewBuffer(make([]byte, 0, 4096))
		return buf
	},
}

// ConcurrencyLimiter restricts the number of requests actively buffering/processing to prevent OOM.
func ConcurrencyLimiter(maxInflight int) gin.HandlerFunc {
	sema := make(chan struct{}, maxInflight)
	return func(c *gin.Context) {
		timeout := viper.GetInt("server.concurrency_timeout_seconds")
		if timeout <= 0 {
			timeout = 5
		}
		select {
		case sema <- struct{}{}:
			defer func() { <-sema }()
			c.Next()
		case <-time.After(time.Duration(timeout) * time.Second):
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": gin.H{
					"message": "Service overloaded, too many concurrent requests",
					"type":    "rate_limit_error",
					"code":    "service_unavailable",
				},
			})
		}
	}
}

// BodyBuffer reads and buffers request body for token count and mutations, then restores it.
func BodyBuffer() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		buf := bufferPool.Get().(*bytes.Buffer)
		buf.Reset()

		// Ensure buffer is returned to the pool after the request completes
		defer bufferPool.Put(buf)

		_, err := io.Copy(buf, c.Request.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
					"error": gin.H{
						"message": "Request body exceeds maximum allowed size",
						"type":    "request_entity_too_large",
						"code":    "payload_too_large",
					},
				})
				return
			}
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"message": fmt.Sprintf("Failed to read body: %v", err),
					"type":    "invalid_request_error",
					"code":    "invalid_request",
				},
			})
			return
		}

		// buf.Bytes() is a slice pointing to the buffer's underlying array.
		// Copy it into a new slice to prevent cross-request race conditions
		// if downstream middleware or async tasks retain the slice after c.Next()
		// when the buffer is returned to the pool.
		bodyBytes := append([]byte(nil), buf.Bytes()...)

		// Restore r.Body for downstream
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		c.Set(RequestBodyCtx, bodyBytes)
		c.Next()
	}
}
