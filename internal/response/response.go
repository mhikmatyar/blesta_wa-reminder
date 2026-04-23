package response

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Meta struct {
	RequestID string `json:"request_id"`
	Timestamp string `json:"timestamp"`
}

type SuccessEnvelope struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data"`
	Meta    Meta        `json:"meta"`
}

type ErrorDetail struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

type ErrorContent struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details []ErrorDetail `json:"details,omitempty"`
}

type ErrorEnvelope struct {
	Success bool         `json:"success"`
	Error   ErrorContent `json:"error"`
	Meta    Meta         `json:"meta"`
}

func OK(c *gin.Context, status int, data interface{}) {
	c.JSON(status, SuccessEnvelope{
		Success: true,
		Data:    data,
		Meta:    buildMeta(c),
	})
}

func Fail(c *gin.Context, status int, code, message string, details []ErrorDetail) {
	c.JSON(status, ErrorEnvelope{
		Success: false,
		Error: ErrorContent{
			Code:    code,
			Message: message,
			Details: details,
		},
		Meta: buildMeta(c),
	})
}

func buildMeta(c *gin.Context) Meta {
	reqID := c.GetString("request_id")
	if reqID == "" {
		reqID = uuid.NewString()
	}
	return Meta{
		RequestID: reqID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}
