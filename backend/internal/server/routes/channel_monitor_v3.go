package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func registerChannelMonitorV3Routes(admin *gin.RouterGroup, h *handler.Handlers, settings *service.SettingService) {
	admin.PUT("/channel-monitor-mode", h.ChannelMonitorV3.SetMode)
	monitor := admin.Group("/channel-monitor-v3", channelMonitorAdminFeatureGuard(settings))
	monitor.GET("/settings", h.ChannelMonitorV3.GetSettings)
	monitor.PUT("/config", h.ChannelMonitorV3.UpdateConfig)
	monitor.POST("/categories", h.ChannelMonitorV3.CreateCategory)
	monitor.PUT("/categories/:id", h.ChannelMonitorV3.UpdateCategory)
	monitor.DELETE("/categories/:id", h.ChannelMonitorV3.DeleteCategory)
	monitor.POST("/components", h.ChannelMonitorV3.CreateComponent)
	monitor.PUT("/components/:id", h.ChannelMonitorV3.UpdateComponent)
	monitor.DELETE("/components/:id", h.ChannelMonitorV3.DeleteComponent)
	monitor.POST("/reorder", h.ChannelMonitorV3.Reorder)
	monitor.GET("/status", h.ChannelMonitorV3.Status)
	monitor.GET("/incidents", h.ChannelMonitorV3.Incidents)
}
