// Package feedback 是意见反馈（6.13，T24）：功能建议、识别不准、批改不准、出现问题、侵权投诉；截图最多 3 张；
// 勾选「允许客服查看相关资料」时写 content_access_grants（72 小时内有效，每次查看都通知用户，PRD 10.1）。回复在消息中心。
package feedback

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/store"
)

// 限制与授权时长。
const (
	maxScreenshots = 3
	minContent     = 5
	maxContent     = 2000
	grantTTL       = 72 * time.Hour
)

var types = map[string]bool{"suggestion": true, "recognition": true, "grading": true, "bug": true, "infringement": true}

type Service struct {
	db  *sql.DB
	q   *dbq.Queries
	now func() time.Time
}

func New(db *sql.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: db, q: dbq.New(db), now: now}
}

// Input 是一条反馈。截图是 /handwriting/upload-requests 上传后的对象键。
type Input struct {
	Type        string
	Content     string
	Screenshots []string
	AllowAccess bool
	MaterialID  uint64
}

// Feedback 是历史记录里的一条。
type Feedback struct {
	ID          uint64
	Type        string
	Content     string
	Screenshots []string
	AllowAccess bool
	Status      string
	Reply       string
	RepliedAt   *time.Time
	CreatedAt   time.Time
}

// Submit 提交反馈。截图只能是自己上传的；关联的资料必须是自己的。
func (s *Service) Submit(ctx context.Context, userID uint64, in Input) (Feedback, error) {
	if !types[in.Type] {
		return Feedback{}, apperr.New(apperr.BadRequest, "请选择反馈类型")
	}
	content := strings.TrimSpace(in.Content)
	if n := len([]rune(content)); n < minContent || n > maxContent {
		return Feedback{}, apperr.New(apperr.BadRequest, "详细描述写 5–2000 字")
	}
	if len(in.Screenshots) > maxScreenshots {
		return Feedback{}, apperr.New(apperr.BadRequest, "截图最多 3 张")
	}
	prefix := "u/" + strconv.FormatUint(userID, 10) + "/"
	for _, k := range in.Screenshots {
		if !strings.HasPrefix(k, prefix) {
			return Feedback{}, apperr.NotFoundErr()
		}
	}
	if in.MaterialID != 0 {
		ok, err := s.q.OwnsMaterial(ctx, dbq.OwnsMaterialParams{ID: in.MaterialID, OwnerUserID: userID})
		if err != nil {
			return Feedback{}, err
		}
		if !ok {
			return Feedback{}, apperr.NotFoundErr()
		}
	}
	shots := in.Screenshots
	if shots == nil {
		shots = []string{}
	}
	raw, _ := json.Marshal(shots)
	now := s.now().UTC()
	var id int64
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		var err error
		id, err = q.InsertFeedback(ctx, dbq.InsertFeedbackParams{OwnerUserID: userID, Ftype: dbq.FeedbacksFtype(in.Type), Content: content, ScreenshotKeys: raw,
			AllowAccess: in.AllowAccess, RelatedMaterialID: sql.NullInt64{Int64: int64(in.MaterialID), Valid: in.MaterialID != 0}, CreatedAt: now})
		if err != nil || !in.AllowAccess {
			return err
		}
		scope := []map[string]any{{"type": "feedback", "id": id}}
		if in.MaterialID != 0 {
			scope = append(scope, map[string]any{"type": "material", "id": in.MaterialID})
		}
		sc, _ := json.Marshal(scope)
		return q.InsertFeedbackGrant(ctx, dbq.InsertFeedbackGrantParams{UserID: userID, SourceID: uint64(id), Scope: sc, GrantedAt: now, ExpiresAt: now.Add(grantTTL)})
	})
	if err != nil {
		return Feedback{}, err
	}
	return Feedback{ID: uint64(id), Type: in.Type, Content: content, Screenshots: shots, AllowAccess: in.AllowAccess, Status: "open", CreatedAt: now}, nil
}

// List 是我的反馈历史（新的在前）。
func (s *Service) List(ctx context.Context, userID uint64) ([]Feedback, error) {
	rows, err := s.q.ListFeedbacks(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Feedback, 0, len(rows))
	for _, r := range rows {
		f := Feedback{ID: r.ID, Type: string(r.Ftype), Content: r.Content, AllowAccess: r.AllowAccess, Status: string(r.Status), Reply: r.Reply.String,
			CreatedAt: r.CreatedAt, Screenshots: []string{}}
		_ = json.Unmarshal(r.ScreenshotKeys, &f.Screenshots)
		if r.RepliedAt.Valid {
			t := r.RepliedAt.Time
			f.RepliedAt = &t
		}
		out = append(out, f)
	}
	return out, nil
}
