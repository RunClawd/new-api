package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// BG per-key rate limiting configuration.
// Defaults: 60 requests per 60 seconds per API key.
var (
	bgPerKeyRateLimitNum      = 60
	bgPerKeyRateLimitDuration = int64(60)
	bgPerKeyRateLimitEnabled  = true
)

func init() {
	if v := os.Getenv("BG_PER_KEY_RATE_LIMIT_NUM"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			bgPerKeyRateLimitNum = n
		}
	}
	if v := os.Getenv("BG_PER_KEY_RATE_LIMIT_DURATION"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			bgPerKeyRateLimitDuration = n
		}
	}
	if v := os.Getenv("BG_PER_KEY_RATE_LIMIT_ENABLE"); v == "false" {
		bgPerKeyRateLimitEnabled = false
	}
}

// BgPerKeyRateLimit returns middleware that rate-limits by API key (token_id from TokenAuth context).
// Must be placed AFTER TokenAuth middleware in the middleware chain.
func BgPerKeyRateLimit() gin.HandlerFunc {
	if !bgPerKeyRateLimitEnabled {
		return defNext
	}

	if common.RedisEnabled {
		return func(c *gin.Context) {
			tokenID := c.GetInt("token_id")
			if tokenID == 0 {
				c.Next()
				return
			}
			key := fmt.Sprintf("BK:token:%d", tokenID)
			redisRateLimiter(c, bgPerKeyRateLimitNum, bgPerKeyRateLimitDuration, key)
		}
	}

	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		tokenID := c.GetInt("token_id")
		if tokenID == 0 {
			c.Next()
			return
		}
		key := fmt.Sprintf("BK:token:%d", tokenID)
		if !inMemoryRateLimiter.Request(key, bgPerKeyRateLimitNum, bgPerKeyRateLimitDuration) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{
					"code":    "rate_limit_exceeded",
					"message": "API key rate limit exceeded. Try again later.",
				},
			})
			c.Abort()
			return
		}
	}
}
