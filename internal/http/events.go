package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/events"
	"peetraining-server/internal/gen"
)

// 埋点上报（T32，PRD 15 节）。

func (h *Handlers) PostEvents(c *gin.Context) {
	var body gen.EventBatch
	if !bind(c, &body) {
		return
	}
	evs := make([]events.Event, len(body.Events))
	for i, e := range body.Events {
		evs[i] = events.Event{Name: e.Name, At: e.At}
		if e.Props != nil {
			evs[i].Props = *e.Props
		}
	}
	n := h.deps.Events.Record(c.Request.Context(), events.Common{UserID: currentUser(c), SubjectCode: deref(body.SubjectCode), Stage: deref(body.Stage),
		AppVersion: body.AppVersion, Platform: string(body.Platform)}, evs)
	c.JSON(http.StatusOK, gin.H{"accepted": n})
}
