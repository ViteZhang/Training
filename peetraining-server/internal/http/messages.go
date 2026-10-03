package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/gen"
)

// 2.3 消息中心（T27）。

func (h *Handlers) ListMessages(c *gin.Context, params gen.ListMessagesParams) {
	cursor := ""
	if params.Cursor != nil {
		cursor = *params.Cursor
	}
	p, err := h.deps.Notify.List(c.Request.Context(), currentUser(c), cursor)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.MessagePage{Unread: p.Unread, Items: make([]gen.Message, len(p.Items))}
	if p.NextCursor != "" {
		out.NextCursor = ptr(p.NextCursor)
	}
	for i, m := range p.Items {
		g := gen.Message{Id: int64(m.ID), Type: gen.MessageType(m.Type), Title: m.Title, Body: m.Body, Read: m.Read, CreatedAt: m.CreatedAt}
		if m.Page != "" {
			g.Page = ptr(m.Page)
			g.Params = &m.Params
		}
		out.Items[i] = g
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) GetUnreadCount(c *gin.Context) {
	n, err := h.deps.Notify.Unread(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"count": n})
}

func (h *Handlers) ReadMessage(c *gin.Context, messageID int64) {
	if err := h.deps.Notify.Read(c.Request.Context(), currentUser(c), uint64(messageID)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) ReadAllMessages(c *gin.Context) {
	if err := h.deps.Notify.ReadAll(c.Request.Context(), currentUser(c)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}
