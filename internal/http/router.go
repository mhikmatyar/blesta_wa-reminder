package http

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"github.com/blesta/wa-reminder/internal/app"
	"github.com/blesta/wa-reminder/internal/http/handler"
	"github.com/blesta/wa-reminder/internal/http/middleware"
	"github.com/blesta/wa-reminder/internal/response"
)

func NewRouter(application *app.App) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.RequestID())

	reminderHandler := handler.NewReminderHandler(application.ReminderService)
	adminHandler := handler.NewAdminHandler(application.AdminService)

	router.GET("/admin", middleware.AdminBasicAuth(application.Config), func(c *gin.Context) {
		c.File(filepath.Join("web", "admin", "index.html"))
	})
	router.Static("/admin/static", filepath.Join("web", "admin"))

	router.GET("/health/live", func(c *gin.Context) {
		response.OK(c, http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/health/ready", func(c *gin.Context) {
		if err := application.Repo.Ping(c.Request.Context()); err != nil {
			response.Fail(c, http.StatusServiceUnavailable, "NOT_READY", err.Error(), nil)
			return
		}
		response.OK(c, http.StatusOK, gin.H{"status": "ready"})
	})

	external := router.Group("/api/v1")
	external.Use(middleware.ExternalBearerAuth(application.Config))
	{
		external.POST("/reminders", reminderHandler.CreateReminder)
		external.POST("/reminders/bulk", reminderHandler.CreateBulkReminder)
		external.GET("/reminders/:job_id", reminderHandler.GetReminder)
		external.POST("/reminders/:job_id/cancel", reminderHandler.CancelReminder)
	}

	admin := router.Group("/admin-api/v1")
	admin.Use(middleware.AdminBasicAuth(application.Config))
	{
		admin.GET("/wa/status", adminHandler.WAStatus)
		admin.GET("/wa/qr", adminHandler.WAQR)
		admin.POST("/wa/qr/refresh", adminHandler.WARefreshQR)
		admin.POST("/wa/reconnect", adminHandler.WAReconnect)
		admin.POST("/wa/logout", adminHandler.WALogout)
		admin.GET("/stats/overview", adminHandler.StatsOverview)
		admin.GET("/deliveries", adminHandler.ListDeliveries)
		admin.GET("/deliveries/export.csv", adminHandler.ExportDeliveriesCSV)
		admin.GET("/deliveries/:id", adminHandler.DeliveryDetail)
		admin.POST("/queue/pause", adminHandler.PauseQueue)
		admin.POST("/queue/resume", adminHandler.ResumeQueue)
	}

	return router
}
