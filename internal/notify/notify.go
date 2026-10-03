// Package notify 是消息中心（2.3，PRD 2.3、10.1）：列表、未读数、已读；保留 30 天；系统消息的批量发送
// （后台公告、协议更新、复习到期）；后台在授权期内查看用户内容时记日志并通知用户；客服回复写到消息中心。
//
// 单条业务消息（解析完成、批改完成等）由各业务在自己的事务里写 messages（dedupe_key 去重）；这里只放消息中心本身
// 与跨用户的批量消息。
package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

// Retention 是消息保留时长（PRD 2.3）。
const Retention = 30 * 24 * time.Hour

// Link 是消息的跳转目标：App 按 page 映射到页面（2.3 点击跳转）。
func Link(page string, params map[string]any) dbtypes.NullJSON {
	if params == nil {
		params = map[string]any{}
	}
	b, _ := json.Marshal(map[string]any{"page": page, "params": params})
	return dbtypes.NullJSON(b)
}

// Service 是消息中心。
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

// Message 是一条消息。
type Message struct {
	ID        uint64
	Type      string
	Title     string
	Body      string
	Page      string
	Params    map[string]any
	Read      bool
	CreatedAt time.Time
}

// Page 是一页消息。
type Page struct {
	Items      []Message
	NextCursor string
	Unread     int
}

const pageSize = 30

// List 返回 30 天内的消息，新的在前；cursor 是上一页最后一条的 ID。
func (s *Service) List(ctx context.Context, userID uint64, cursor string) (Page, error) {
	before, _ := strconv.ParseUint(cursor, 10, 64)
	since := s.now().UTC().Add(-Retention)
	rows, err := s.q.ListMessages(ctx, dbq.ListMessagesParams{Owner: userID, Since: since, BeforeID: before, Limit: pageSize + 1})
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: make([]Message, 0, min(len(rows), pageSize))}
	for i, r := range rows {
		if i == pageSize {
			out.NextCursor = strconv.FormatUint(rows[i-1].ID, 10)
			break
		}
		m := Message{ID: r.ID, Type: string(r.Mtype), Title: r.Title, Body: r.Body, Read: r.ReadAt.Valid, CreatedAt: r.CreatedAt}
		if len(r.Link) > 0 {
			var l struct {
				Page   string         `json:"page"`
				Params map[string]any `json:"params"`
			}
			if json.Unmarshal(r.Link, &l) == nil {
				m.Page, m.Params = l.Page, l.Params
			}
		}
		out.Items = append(out.Items, m)
	}
	n, err := s.Unread(ctx, userID)
	out.Unread = n
	return out, err
}

// Unread 是 30 天内的未读数（首页铃铛红点）。
func (s *Service) Unread(ctx context.Context, userID uint64) (int, error) {
	n, err := s.q.CountUnreadMessages(ctx, dbq.CountUnreadMessagesParams{OwnerUserID: userID, CreatedAt: s.now().UTC().Add(-Retention)})
	return int(n), err
}

// Read 标一条已读；别人的消息返回 404。
func (s *Service) Read(ctx context.Context, userID, id uint64) error {
	if _, err := s.q.GetMessageOwned(ctx, dbq.GetMessageOwnedParams{ID: id, OwnerUserID: userID}); errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	} else if err != nil {
		return err
	}
	_, err := s.q.MarkMessageRead(ctx, dbq.MarkMessageReadParams{ReadAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, ID: id, OwnerUserID: userID})
	return err
}

// ReadAll 全部已读。
func (s *Service) ReadAll(ctx context.Context, userID uint64) error {
	_, err := s.q.MarkAllMessagesRead(ctx, dbq.MarkAllMessagesReadParams{ReadAt: sql.NullTime{Time: s.now().UTC(), Valid: true}, OwnerUserID: userID})
	return err
}

// Cleanup 删除 30 天前的消息（定时任务，每次最多删 5000 条，删不完下次继续）。
func (s *Service) Cleanup(ctx context.Context) (int, error) {
	total := 0
	for range 20 {
		n, err := s.q.DeleteOldMessages(ctx, s.now().UTC().Add(-Retention))
		total += int(n)
		if err != nil || n < 5000 {
			return total, err
		}
	}
	return total, nil
}

// audience 是公告的发送对象（7.9）：全部用户，或指定用户 ID。
type audience struct {
	All     bool     `json:"all"`
	UserIDs []uint64 `json:"user_ids"`
}

// SendAnnouncements 发送到点的后台公告（定时任务）。重复执行时 dedupe_key 保证每人一条。
func (s *Service) SendAnnouncements(ctx context.Context) (int, error) {
	now := s.now().UTC()
	rows, err := s.q.ListDueAnnouncements(ctx, sql.NullTime{Time: now, Valid: true})
	if err != nil {
		return 0, err
	}
	total := 0
	for _, a := range rows {
		var au audience
		_ = json.Unmarshal(a.Audience, &au)
		key := sql.NullString{String: "announcement:" + strconv.FormatUint(a.ID, 10), Valid: true}
		var n int64
		switch {
		case au.All:
			n, err = s.q.SendAnnouncementToAll(ctx, dbq.SendAnnouncementToAllParams{Title: a.Title, Body: a.Body, DedupeKey: key})
		case len(au.UserIDs) > 0:
			ids, _ := json.Marshal(au.UserIDs)
			n, err = s.q.SendAnnouncementToUsers(ctx, dbq.SendAnnouncementToUsersParams{Title: a.Title, Body: a.Body, DedupeKey: key, UserIds: string(ids)})
		}
		if err != nil {
			return total, err
		}
		if err := s.q.MarkAnnouncementSent(ctx, dbq.MarkAnnouncementSentParams{SentAt: sql.NullTime{Time: now, Valid: true}, SentCount: uint32(n), ID: a.ID}); err != nil {
			return total, err
		}
		total += int(n)
	}
	return total, nil
}

var agreementNames = map[dbq.AgreementsKind]string{dbq.AgreementsKindUser: "用户协议", dbq.AgreementsKindPrivacy: "隐私政策", dbq.AgreementsKindMembership: "会员服务协议"}

// SendAgreementUpdates 给还没同意新版本的老用户发「协议更新」（定时任务；新版本发布 30 天内）。
// App 下次启动时还会弹 0.4b 让用户确认。
func (s *Service) SendAgreementUpdates(ctx context.Context) (int, error) {
	now := s.now().UTC()
	rows, err := s.q.ListRecentAgreements(ctx, dbq.ListRecentAgreementsParams{PublishedAt: sql.NullTime{Time: now, Valid: true},
		PublishedAt_2: sql.NullTime{Time: now.Add(-Retention), Valid: true}})
	if err != nil {
		return 0, err
	}
	total := 0
	for _, a := range rows {
		body := agreementNames[a.Kind] + "已更新到 " + a.Version + " 版"
		if a.ChangeSummary.Valid && a.ChangeSummary.String != "" {
			body += "：" + a.ChangeSummary.String
		}
		n, err := s.q.SendAgreementUpdate(ctx, dbq.SendAgreementUpdateParams{Title: agreementNames[a.Kind] + "更新", Body: body,
			Link: Link("agreement", map[string]any{"kind": string(a.Kind)}), DedupeKey: sql.NullString{String: "agreement:" + strconv.FormatUint(a.ID, 10), Valid: true}, AgreementID: a.ID})
		if err != nil {
			return total, err
		}
		total += int(n)
	}
	return total, nil
}

// SendReviewDue 发今天的复习到期提醒（定时任务，每天早上一次；打开了「复习到期提醒」的用户）。
func (s *Service) SendReviewDue(ctx context.Context) (int, error) {
	day := rules.DayOf(s.now())
	n, err := s.q.SendReviewDue(ctx, dbq.SendReviewDueParams{Day: sql.NullTime{Time: day.Date(), Valid: true}, Link: Link("today", nil),
		DedupeKey: sql.NullString{String: "review_due:" + day.Date().Format("2006-01-02"), Valid: true}})
	return int(n), err
}

// Target 是后台要查看的一个用户内容对象。
type Target struct {
	Type string `json:"type"`
	ID   uint64 `json:"id"`
}

// targetNames 是通知里对象的说法。
var targetNames = map[string]string{"material": "资料", "question": "题目", "grading": "批改记录", "attempt": "作答", "essay": "作文", "import_job": "解析任务"}

// ErrNoGrant 表示没有有效授权或对象不在授权范围内。
var ErrNoGrant = apperr.New(apperr.Forbidden, "用户没有授权查看这项内容，或授权已过期")

// RecordAccess 在后台读取授权内容之前调用（PRD 10.1、ADR 0006）：授权必须在有效期内、对象在授权范围内；
// 每次查看都写 content_access_logs 并发消息告诉用户。返回授权所属的用户，调用方只能读这个用户的这一个对象。
func (s *Service) RecordAccess(ctx context.Context, adminID, grantID uint64, t Target) (uint64, error) {
	now := s.now().UTC()
	var userID uint64
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		g, err := q.GetActiveGrant(ctx, dbq.GetActiveGrantParams{ID: grantID, ExpiresAt: now})
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoGrant
		}
		if err != nil {
			return err
		}
		var scope []Target
		if err := json.Unmarshal(g.Scope, &scope); err != nil || !slices.Contains(scope, t) {
			return ErrNoGrant
		}
		userID = g.UserID
		if err := q.InsertContentAccessLog(ctx, dbq.InsertContentAccessLogParams{GrantID: g.ID, AdminID: adminID, UserID: g.UserID, TargetType: t.Type,
			TargetID: t.ID, CreatedAt: now}); err != nil {
			return err
		}
		name := targetNames[t.Type]
		if name == "" {
			name = "内容"
		}
		// 每次查看都通知；同一对象在同一分钟内的连续请求（翻页、加载附件）合并成一条。
		return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: g.UserID, Mtype: dbq.MessagesMtypeContentAccessed, Title: "客服查看了你授权的" + name,
			Body:      "根据你的授权，客服在 " + now.In(shanghai).Format("01-02 15:04") + " 查看了相关" + name + "，用于排查问题；授权至 " + g.ExpiresAt.In(shanghai).Format("01-02 15:04") + " 失效",
			DedupeKey: sql.NullString{String: "access:" + strconv.FormatUint(g.ID, 10) + ":" + t.Type + ":" + strconv.FormatUint(t.ID, 10) + ":" + now.Format("200601021504"), Valid: true}})
	})
	return userID, err
}

var shanghai = time.FixedZone("Asia/Shanghai", 8*3600)

// Reply 是客服回复用户反馈（7.7）：写回反馈，并发到用户的消息中心。
func (s *Service) Reply(ctx context.Context, adminID, feedbackID uint64, reply string) error {
	now := s.now().UTC()
	return store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		owner, err := q.GetFeedbackOwner(ctx, feedbackID)
		if errors.Is(err, sql.ErrNoRows) {
			return apperr.NotFoundErr()
		}
		if err != nil {
			return err
		}
		if _, err := q.ReplyFeedback(ctx, dbq.ReplyFeedbackParams{Reply: sql.NullString{String: reply, Valid: true}, RepliedBy: sql.NullInt64{Int64: int64(adminID), Valid: true},
			RepliedAt: sql.NullTime{Time: now, Valid: true}, ID: feedbackID}); err != nil {
			return err
		}
		return q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: owner, Mtype: dbq.MessagesMtypeSupportReply, Title: "客服回复了你的反馈", Body: reply,
			Link:      Link("feedback", map[string]any{"feedback_id": feedbackID}),
			DedupeKey: sql.NullString{String: "reply:" + strconv.FormatUint(feedbackID, 10) + ":" + strconv.FormatInt(now.UnixMilli(), 10), Valid: true}})
	})
}
