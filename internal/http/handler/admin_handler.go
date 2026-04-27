package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/blesta/wa-reminder/internal/domain/model"
	"github.com/blesta/wa-reminder/internal/repository/postgres"
	"github.com/blesta/wa-reminder/internal/response"
	"github.com/blesta/wa-reminder/internal/service"
)

type AdminHandler struct {
	service *service.AdminService
}

func NewAdminHandler(service *service.AdminService) *AdminHandler {
	return &AdminHandler{service: service}
}

func (h *AdminHandler) WAStatus(c *gin.Context) {
	data, err := h.service.GetWAStatus(c.Request.Context())
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, data)
}

func (h *AdminHandler) WAQR(c *gin.Context) {
	data, err := h.service.GetWAQR(c.Request.Context())
	if err != nil {
		if errors.Is(err, service.ErrQRCodeUnavailable) {
			response.OK(c, http.StatusOK, model.WAQRCode{
				QRCode:           "",
				ExpiresInSeconds: 0,
			})
			return
		}
		response.Fail(c, http.StatusConflict, "CONFLICT", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, data)
}

func (h *AdminHandler) WAReconnect(c *gin.Context) {
	if err := h.service.ReconnectWA(c.Request.Context()); err != nil {
		response.Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, gin.H{"status": "reconnecting"})
}

func (h *AdminHandler) WARefreshQR(c *gin.Context) {
	if err := h.service.RefreshWAQR(c.Request.Context()); err != nil {
		response.Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, gin.H{"status": "qr_refreshed"})
}

func (h *AdminHandler) WALogout(c *gin.Context) {
	if err := h.service.LogoutWA(c.Request.Context()); err != nil {
		response.Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, gin.H{"status": "logged_out"})
}

func (h *AdminHandler) StatsOverview(c *gin.Context) {
	rng := c.DefaultQuery("range", "today")
	data, err := h.service.GetStatsOverview(c.Request.Context(), rng)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, data)
}

func (h *AdminHandler) ListDeliveries(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	status := c.Query("status")
	search := c.Query("search")
	from := c.Query("from")
	to := c.Query("to")

	data, err := h.service.ListDeliveriesAdvanced(c.Request.Context(), status, search, from, to, page, limit)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, data)
}

func (h *AdminHandler) DeliveryDetail(c *gin.Context) {
	data, err := h.service.GetDelivery(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			response.Fail(c, http.StatusNotFound, "NOT_FOUND", "delivery not found", nil)
			return
		}
		response.Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, data)
}

func (h *AdminHandler) PauseQueue(c *gin.Context) {
	if err := h.service.PauseQueue(c.Request.Context()); err != nil {
		response.Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, gin.H{"queue_paused": true})
}

func (h *AdminHandler) ResumeQueue(c *gin.Context) {
	if err := h.service.ResumeQueue(c.Request.Context()); err != nil {
		response.Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	response.OK(c, http.StatusOK, gin.H{"queue_paused": false})
}

func (h *AdminHandler) ExportDeliveriesCSV(c *gin.Context) {
	status := c.Query("status")
	search := c.Query("search")
	from := c.Query("from")
	to := c.Query("to")

	data, filename, err := h.service.ExportDeliveriesCSV(c.Request.Context(), status, search, from, to)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), nil)
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}
