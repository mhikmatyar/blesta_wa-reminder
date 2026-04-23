package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/blesta/wa-reminder/internal/domain/model"
	"github.com/blesta/wa-reminder/internal/repository/postgres"
	"github.com/blesta/wa-reminder/internal/response"
	"github.com/blesta/wa-reminder/internal/service"
)

type ReminderHandler struct {
	service *service.ReminderService
}

func NewReminderHandler(service *service.ReminderService) *ReminderHandler {
	return &ReminderHandler{service: service}
}

func (h *ReminderHandler) CreateReminder(c *gin.Context) {
	var req model.CreateReminderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error(), nil)
		return
	}

	idempotencyKey := c.GetHeader("Idempotency-Key")
	job, err := h.service.CreateReminder(c.Request.Context(), req, idempotencyKey)
	if err != nil {
		response.Fail(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error(), nil)
		return
	}

	response.OK(c, http.StatusCreated, gin.H{
		"job_id":    job.JobUUID,
		"status":    job.Status,
		"queued_at": job.CreatedAt,
	})
}

func (h *ReminderHandler) CreateBulkReminder(c *gin.Context) {
	var req model.CreateBulkReminderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error(), nil)
		return
	}
	idempotency := c.GetHeader("Idempotency-Key")
	results, err := h.service.CreateBulkReminder(c.Request.Context(), req, idempotency)
	if err != nil {
		response.Fail(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", err.Error(), nil)
		return
	}

	accepted := 0
	rejected := 0
	for _, item := range results {
		if _, ok := item["error"]; ok {
			rejected++
		} else {
			accepted++
		}
	}
	response.OK(c, http.StatusOK, gin.H{
		"total":    len(results),
		"accepted": accepted,
		"rejected": rejected,
		"results":  results,
	})
}

func (h *ReminderHandler) GetReminder(c *gin.Context) {
	job, err := h.service.GetReminder(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			response.Fail(c, http.StatusNotFound, "NOT_FOUND", "job not found", nil)
			return
		}
		response.Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, job)
}

func (h *ReminderHandler) CancelReminder(c *gin.Context) {
	err := h.service.CancelReminder(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			response.Fail(c, http.StatusConflict, "CONFLICT", "job cannot be cancelled or not found", nil)
			return
		}
		response.Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, gin.H{
		"job_id": c.Param("job_id"),
		"status": model.ReminderStatusCancelled,
	})
}
