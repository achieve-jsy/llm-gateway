package middleware

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				requestID, _ := c.Get("request_id")
				slog.Error("panic recovered", "request_id", requestID, "error", r)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": gin.H{"type": "internal_error", "message": "internal server error", "code": "internal_error"},
				})
			}
		}()
		c.Next()
	}
}
