package admin

import (
	"context"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *ProxyHandler) proxyGroups(c *gin.Context) service.ProxyGroupAdminService {
	svc, ok := h.adminService.(service.ProxyGroupAdminService)
	if !ok {
		response.InternalError(c, "Proxy group management unavailable")
		return nil
	}
	return svc
}

func (h *ProxyHandler) ListGroups(c *gin.Context) {
	svc := h.proxyGroups(c)
	if svc == nil {
		return
	}
	groups, err := svc.ListProxyGroups(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, groups)
}

func (h *ProxyHandler) CreateGroup(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	svc := h.proxyGroups(c)
	if svc == nil {
		return
	}
	group, err := svc.CreateProxyGroup(c.Request.Context(), req.Name)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, group)
}

func proxyGroupID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid proxy group ID")
		return 0, false
	}
	return id, true
}

func (h *ProxyHandler) UpdateGroup(c *gin.Context) {
	id, ok := proxyGroupID(c)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	svc := h.proxyGroups(c)
	if svc == nil {
		return
	}
	group, err := svc.UpdateProxyGroup(c.Request.Context(), id, req.Name)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, group)
}

func (h *ProxyHandler) DeleteGroup(c *gin.Context) {
	id, ok := proxyGroupID(c)
	if !ok {
		return
	}
	svc := h.proxyGroups(c)
	if svc == nil {
		return
	}
	if err := svc.DeleteProxyGroup(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Proxy group deleted successfully"})
}

func (h *ProxyHandler) BatchGroup(c *gin.Context) {
	var req struct {
		IDs     []int64                `json:"ids" binding:"required,min=1,max=1000,dive,gt=0"`
		GroupID dto.NullableInt64Field `json:"group_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if !req.GroupID.Set {
		response.BadRequest(c, "group_id is required; use null to remove group membership")
		return
	}
	svc := h.proxyGroups(c)
	if svc == nil {
		return
	}
	count, err := svc.AssignProxyGroup(c.Request.Context(), req.IDs, req.GroupID.Value)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"updated_count": count})
}

func proxyGroupFilterContext(c *gin.Context) (context.Context, bool) {
	ctx := c.Request.Context()
	value, present := c.GetQuery("group_id")
	if !present {
		return ctx, true
	}
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id < 0 {
		response.BadRequest(c, "Invalid proxy group filter")
		return ctx, false
	}
	return service.WithProxyGroupFilter(ctx, id), true
}
