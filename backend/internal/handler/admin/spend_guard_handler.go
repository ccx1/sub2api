package admin

import (
	"errors"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type SpendGuardHandler struct {
	service *service.SpendGuardService
}

func NewSpendGuardHandler(svc *service.SpendGuardService) *SpendGuardHandler {
	return &SpendGuardHandler{service: svc}
}

func (h *SpendGuardHandler) requireService(c *gin.Context) bool {
	if h == nil || h.service == nil {
		response.ErrorFrom(c, errors.New("spend guard service unavailable"))
		return false
	}
	return true
}

func (h *SpendGuardHandler) Offenders(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	items, err := h.service.Offenders(c.Request.Context(), 0)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if items == nil {
		items = []service.SpendGuardOffender{}
	}
	response.Success(c, gin.H{"items": items, "count": len(items)})
}

func (h *SpendGuardHandler) GetSettings(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	response.Success(c, h.service.GetSettings(c.Request.Context()))
}

func (h *SpendGuardHandler) UpdateSettings(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	var req service.SpendGuardSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.service.UpdateSettings(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *SpendGuardHandler) Events(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	events, err := h.service.Events(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if events == nil {
		events = []service.SpendGuardEvent{}
	}
	response.Success(c, gin.H{"items": events, "count": len(events)})
}

func (h *SpendGuardHandler) Unfreeze(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid API key ID")
		return
	}
	if err := h.service.Unfreeze(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "API key unfrozen successfully"})
}
