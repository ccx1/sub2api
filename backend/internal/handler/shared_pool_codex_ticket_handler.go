package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *SharedPoolHandler) SetCodexTicketEnabled(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	id, ok := sharedID(c)
	if !ok {
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !sharedBind(c, &input) {
		return
	}
	if input.Enabled == nil {
		response.BadRequest(c, "请明确设置是否开启打票")
		return
	}
	if err := h.pool.SetCodexTicketEnabled(c.Request.Context(), userID, id, *input.Enabled); err != nil {
		sharedReply(c, nil, err)
		return
	}
	data, err := h.pool.Get(c.Request.Context(), userID, id)
	sharedReply(c, data, err)
}
