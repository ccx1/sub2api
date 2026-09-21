package handler

import "github.com/gin-gonic/gin"

func (h *SharedPoolHandler) Overview(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	data, err := h.pool.Overview(ctx, userID, h.sharedTicketConfig(ctx))
	if err == nil && h.keys != nil {
		h.keys.EnrichSharedPoolOverviewConcurrency(ctx, data, h.sharedTicketConfig(ctx))
	}
	sharedReply(c, data, err)
}
