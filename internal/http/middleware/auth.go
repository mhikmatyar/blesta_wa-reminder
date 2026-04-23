package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/blesta/wa-reminder/internal/config"
	"github.com/blesta/wa-reminder/internal/response"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := c.GetHeader("X-Request-ID")
		if reqID == "" {
			reqID = uuid.NewString()
		}
		c.Set("request_id", reqID)
		c.Writer.Header().Set("X-Request-ID", reqID)
		c.Next()
	}
}

func ExternalBearerAuth(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] != cfg.APIBearerToken {
			response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "invalid bearer token", nil)
			c.Abort()
			return
		}
		c.Next()
	}
}

func AdminBasicAuth(cfg *config.Config) gin.HandlerFunc {
	return gin.BasicAuth(gin.Accounts{
		cfg.AdminBasicUser: cfg.AdminBasicPass,
	})
}
