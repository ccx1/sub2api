package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func registerSharedPoolRoutes(authenticated *gin.RouterGroup, h *handler.SharedPoolHandler) {
	if h == nil {
		return
	}
	p := authenticated.Group("/shared-pool")
	p.GET("/overview", h.Overview)
	p.GET("/pools", h.Pools)
	p.GET("/config", h.Config)
	p.GET("/accounts", h.Accounts)
	p.POST("/accounts", h.Create)
	p.POST("/accounts/import", h.ImportAccounts)
	p.PUT("/accounts/:id", h.Update)
	p.DELETE("/accounts/:id", h.Remove)
	p.POST("/accounts/:id/enabled", h.SetEnabled)
	p.POST("/accounts/:id/protection", h.SetProtection)
	p.POST("/accounts/:id/codex-ticket", h.SetCodexTicketEnabled)
	p.GET("/accounts/:id/models", h.GetAvailableModels)
	p.GET("/accounts/:id/usage", h.GetUsage)
	p.POST("/accounts/:id/quota/refresh", h.RefreshQuota)
	p.POST("/accounts/:id/reset-quota", h.ResetQuota)
	p.POST("/accounts/:id/test", h.Test)
	p.GET("/summary", h.Summary)
	p.GET("/earnings", h.Earnings)
	p.POST("/transfer", h.Transfer)
	p.POST("/oauth/:platform/start", h.OAuthStart)
	p.POST("/oauth/:platform/finish", h.OAuthFinish)
}

func registerAdminSharedPoolRoutes(admin *gin.RouterGroup, h *handler.SharedPoolHandler) {
	if h == nil {
		return
	}
	p := admin.Group("/shared-pool")
	p.GET("/settings", h.AdminSettings)
	p.PUT("/settings", h.AdminSaveSettings)
	p.GET("/accounts", h.AdminAccounts)
	p.PUT("/accounts/:id", h.AdminAssign)
	p.GET("/user-rates", h.AdminUserRates)
	p.PUT("/user-rates/:id", h.AdminSaveUserRate)
	p.GET("/earnings", h.AdminEarnings)
}
