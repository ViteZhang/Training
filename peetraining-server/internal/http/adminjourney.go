package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/admin"
	"peetraining-server/internal/gen"
)

// 7.20 激活漏斗与 7.2 用户详情的「使用进度」：注册用户用到了哪一步（第一期只用业务表推算）。

func toGenJourney(j admin.Journey) gen.AdminJourney {
	out := gen.AdminJourney{UserId: int64(j.UserID), PhoneMasked: j.PhoneMasked, CreatedAt: j.CreatedAt, LastActiveAt: j.LastActiveAt, LastAnswerAt: j.LastAnswerAt,
		Current: gen.ActivationStage(j.Current), Milestones: make([]gen.AdminMilestone, len(j.Milestones))}
	for i, m := range j.Milestones {
		out.Milestones[i] = gen.AdminMilestone{Key: gen.ActivationStage(m.Key), Label: m.Label, Reached: m.Reached, At: m.At}
	}
	return out
}

func (h *Handlers) GetAdminActivation(c *gin.Context, p gen.GetAdminActivationParams) {
	a, err := h.deps.Admin.Activation(c.Request.Context(), p.From.Time, p.To.Time)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := gen.AdminActivation{Total: a.Total, Steps: make([]gen.AdminActivationStep, len(a.Steps))}
	_ = out.From.UnmarshalText([]byte(a.From))
	_ = out.To.UnmarshalText([]byte(a.To))
	for i, s := range a.Steps {
		st := gen.AdminActivationStep{Key: gen.ActivationStage(s.Key), Label: s.Label, Hint: s.Hint, Reached: s.Reached, Stuck: s.Stuck}
		if s.MedianHours != nil {
			v := fl(*s.MedianHours)
			st.MedianHours = &v
		}
		out.Steps[i] = st
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handlers) ListAdminActivationUsers(c *gin.Context, p gen.ListAdminActivationUsersParams) {
	rows, err := h.deps.Admin.ActivationUsers(c.Request.Context(), p.From.Time, p.To.Time, string(p.Stage), cursorID(p.Cursor))
	if err != nil {
		_ = c.Error(err)
		return
	}
	items := make([]gen.AdminJourney, len(rows))
	for i, j := range rows {
		items[i] = toGenJourney(j)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor(rows, func(j admin.Journey) uint64 { return j.UserID })})
}

func (h *Handlers) GetAdminUserJourney(c *gin.Context, userID int64) {
	j, err := h.deps.Admin.UserJourney(c.Request.Context(), uint64(userID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	base := toGenJourney(j.Journey)
	out := gen.AdminUserJourney{UserId: base.UserId, PhoneMasked: base.PhoneMasked, CreatedAt: base.CreatedAt, LastActiveAt: base.LastActiveAt, LastAnswerAt: base.LastAnswerAt,
		Current: base.Current, Milestones: base.Milestones, ActiveDays30: j.ActiveDays30, Answers30: j.Answers30, Days: make([]gen.AdminDayAnswers, len(j.Days))}
	for i, d := range j.Days {
		out.Days[i] = gen.AdminDayAnswers{Answers: d.Answers}
		if err := out.Days[i].Day.UnmarshalText([]byte(d.Day)); err != nil {
			_ = c.Error(err)
			return
		}
	}
	c.JSON(http.StatusOK, out)
}
