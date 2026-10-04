package official

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/importer"
	"peetraining-server/internal/notify"
	"peetraining-server/internal/store"
)

// ---------- 7.11 立项与进度 ----------

// Project 是一个官方题库项目。
type Project struct {
	ID            uint64
	School        string
	Major         string
	SubjectCode   string
	SubjectName   string
	BankID        uint64
	Stage         string
	EditorIDs     []uint64
	Materials     []Material
	KPCount       int
	QuestionCount int
	ExamCount     int
	Version       string
	Subscribers   int
	CreatedAt     time.Time
}

// Material 是授权资料清单的一项：须有授权或为公开真题（7.11）。
type Material struct {
	Name  string `json:"name"`
	Basis string `json:"basis"` // authorized 已获授权 / public_exam 公开真题
	Note  string `json:"note,omitempty"`
}

// Stages 是进度条：立项 → 授权资料入库 → 知识框架 → 内容生产 → 审核 → 发布上线。
var Stages = []string{"initiated", "materials", "framework", "producing", "reviewing", "published"}

func decodeIDs(b []byte) []uint64 {
	var ids []uint64
	_ = json.Unmarshal(b, &ids)
	if ids == nil {
		ids = []uint64{}
	}
	return ids
}

func (s *Service) Projects(ctx context.Context) ([]Project, error) {
	rows, err := s.q.ListOfficialProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Project, len(rows))
	for i, r := range rows {
		p := Project{ID: r.ID, School: r.School, Major: r.Major, SubjectCode: r.SubjectCode, SubjectName: r.SubjectName, BankID: uint64(r.BankID.Int64), Stage: string(r.Stage),
			EditorIDs: decodeIDs(r.EditorIds), KPCount: int(r.KpCount), QuestionCount: int(r.QuestionCount), ExamCount: int(r.ExamCount), Version: str(r.Version),
			Subscribers: int(r.Subscribers), CreatedAt: r.CreatedAt, Materials: []Material{}}
		_ = json.Unmarshal(r.AuthorizedMaterials, &p.Materials)
		if p.Materials == nil {
			p.Materials = []Material{}
		}
		out[i] = p
	}
	return out, nil
}

func (s *Service) Project(ctx context.Context, id uint64) (Project, error) {
	ps, err := s.Projects(ctx)
	if err != nil {
		return Project{}, err
	}
	for _, p := range ps {
		if p.ID == id {
			return p, nil
		}
	}
	return Project{}, apperr.NotFoundErr()
}

// CreateProject 立项（7.10「立项做官方题库」或 7.11 新建）：同时建好官方题库。
func (s *Service) CreateProject(ctx context.Context, adminID uint64, school, major, code, name string, editors []uint64) (uint64, error) {
	if strings.TrimSpace(school) == "" || strings.TrimSpace(major) == "" || strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" {
		return 0, apperr.New(apperr.BadRequest, "院校、专业、专业课代码和名称都要填")
	}
	ids, _ := json.Marshal(nonNil(editors))
	var id int64
	err := store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		bank, err := q.InsertOfficialBank(ctx, dbq.InsertOfficialBankParams{Title: school + " " + code + " " + name, SchoolMajorTag: nullStr(school + " " + major), SubjectCode: nullStr(code)})
		if err != nil {
			return err
		}
		id, err = q.InsertOfficialProject(ctx, dbq.InsertOfficialProjectParams{School: school, Major: major, SubjectCode: code, SubjectName: name,
			BankID: sql.NullInt64{Int64: bank, Valid: true}, EditorIds: ids, CreatedBy: sql.NullInt64{Int64: int64(adminID), Valid: true}})
		if err != nil {
			return err
		}
		return q.DeleteDemandMark(ctx, code)
	})
	return uint64(id), err
}

func nonNil(ids []uint64) []uint64 {
	if ids == nil {
		return []uint64{}
	}
	return ids
}

// UpdateProject 分配编辑、维护授权资料清单、推进阶段（「发布上线」只能由发布接口设置）。
func (s *Service) UpdateProject(ctx context.Context, id uint64, editors []uint64, materials []Material, stage string) error {
	p, err := s.q.GetOfficialProject(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	if err != nil {
		return err
	}
	if !slices.Contains(Stages, stage) || (stage == "published" && p.Stage != dbq.OfficialProjectsStagePublished) {
		return apperr.New(apperr.BadRequest, "阶段不对（发布上线由「版本发布」完成）")
	}
	for _, m := range materials {
		if m.Basis != "authorized" && m.Basis != "public_exam" {
			return apperr.New(apperr.BadRequest, "授权资料须注明「已获授权」或「公开真题」")
		}
	}
	ids, _ := json.Marshal(nonNil(editors))
	if materials == nil {
		materials = []Material{}
	}
	ms, _ := json.Marshal(materials)
	return s.q.UpdateOfficialProject(ctx, dbq.UpdateOfficialProjectParams{EditorIds: ids, AuthorizedMaterials: ms, Stage: dbq.OfficialProjectsStage(stage), ID: id})
}

// IsEditorOf 判断后台账号是不是这个项目的编辑（内容编辑只能访问被分配的项目）。
func (s *Service) IsEditorOf(ctx context.Context, adminID, projectID uint64) (bool, error) {
	p, err := s.q.GetOfficialProject(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, apperr.NotFoundErr()
	}
	if err != nil {
		return false, err
	}
	return slices.Contains(decodeIDs(p.EditorIds), adminID), nil
}

// Item 是官方题库里的一个条目（编辑选「修订 / 下线」的对象，内容为草稿格式）。
type Item struct {
	EntityType string
	ID         uint64
	Title      string
	Status     string
	Payload    json.RawMessage
}

// Items 列出项目官方题库的当前知识点与题目（是官方内容，不是用户内容）。
func (s *Service) Items(ctx context.Context, projectID uint64) ([]Item, error) {
	p, err := s.Project(ctx, projectID)
	if err != nil {
		return nil, err
	}
	c, err := loadContent(ctx, s.q, p.BankID)
	if err != nil {
		return nil, err
	}
	out := []Item{}
	for _, k := range c.kps {
		if k.Level == dbq.KnowledgePointsLevelPoint {
			out = append(out, Item{EntityType: "knowledge_point", ID: k.ID, Title: k.Name, Status: "active", Payload: c.payloadOf("knowledge_point", k.ID)})
		}
	}
	for _, x := range c.questions {
		pl := c.payloadOf("question", x.ID)
		out = append(out, Item{EntityType: "question", ID: x.ID, Title: titleOf("question", pl), Status: string(x.Status), Payload: pl})
	}
	return out, nil
}

// ---------- 7.10 需求洞察（只用统计） ----------

// Demand 是一门专业课（按代码聚合）的需求。
type Demand struct {
	Code         string
	Users        int
	AvgQuestions int
	WeekNew      int
	WithTarget   int
	PaidRate     float64
	AvgReview    int
	ProjectID    uint64
	ProjectStage string
	Unplanned    bool
}

// Demands 按专业课代码列出自建用户数等统计（只有计数，不读用户资料内容）。threshold 是达到立项门槛的人数。
func (s *Service) Demands(ctx context.Context, weekStart time.Time) ([]Demand, error) {
	rows, err := s.q.StatDemand(ctx, weekStart)
	if err != nil {
		return nil, err
	}
	projects, err := s.q.ListOfficialProjects(ctx)
	if err != nil {
		return nil, err
	}
	marks, err := s.q.ListDemandMarks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Demand, 0, len(rows))
	for _, r := range rows {
		d := Demand{Code: r.Code.String, Users: int(r.Users), AvgQuestions: int(r.AvgQuestions), WeekNew: int(r.WeekNew), WithTarget: int(r.WithTarget), AvgReview: int(r.AvgReview),
			Unplanned: slices.Contains(marks, r.Code.String)}
		if r.Users > 0 {
			d.PaidRate = float64(r.Paid) / float64(r.Users)
		}
		for _, p := range projects {
			if p.SubjectCode == d.Code {
				d.ProjectID, d.ProjectStage = p.ID, string(p.Stage)
				break
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// MarkUnplanned 标为未规划 / 取消标记（7.10）。
func (s *Service) MarkUnplanned(ctx context.Context, adminID uint64, code string, unplanned bool) error {
	if unplanned {
		return s.q.UpsertDemandMark(ctx, dbq.UpsertDemandMarkParams{SubjectCode: code, MarkedBy: sql.NullInt64{Int64: int64(adminID), Valid: true}})
	}
	return s.q.DeleteDemandMark(ctx, code)
}

// ---------- 7.12 内容生产 ----------

// Candidate 是 AI 从资料里拆出的一个知识点候选，已与现有官方知识点树对齐：
// new 新增 / supplement 补充来源（同名同表述）/ differ 表述差异（同名不同表述，须选以哪个为准或并存）。
type Candidate struct {
	KPPayload
	Alignment    string
	ExistingID   uint64
	ExistingText string
}

// KPCandidates 用 AI 从编辑粘贴的资料原文里拆知识点（复用导入引擎的知识点提取能力），并与现有知识点树对齐。
func (s *Service) KPCandidates(ctx context.Context, projectID uint64, text string) ([]Candidate, error) {
	p, err := s.q.GetOfficialProject(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.NotFoundErr()
	}
	if err != nil {
		return nil, err
	}
	if len([]rune(strings.TrimSpace(text))) < 10 {
		return nil, apperr.New(apperr.BadRequest, "资料原文太短")
	}
	out, _, err := ai.ExtractKPs.Run(ctx, s.d.AI, 0, ai.KPIn{Subject: p.SubjectName, Pages: []ai.KPPage{{No: 1, Text: text}}})
	if err != nil {
		return nil, apperr.New(apperr.AIFailed, "AI 拆知识点失败，请稍后再试").Wrap(err)
	}
	kps, err := s.q.ListOfficialKPs(ctx, uint64(p.BankID.Int64))
	if err != nil {
		return nil, err
	}
	existing := map[string]dbq.ListOfficialKPsRow{}
	for _, k := range kps {
		if k.Level == dbq.KnowledgePointsLevelPoint {
			existing[k.Name] = k
		}
	}
	res := make([]Candidate, 0, len(out.Points))
	for _, it := range out.Points {
		c := Candidate{KPPayload: KPPayload{Name: it.Name, OriginalText: it.OriginalText}, Alignment: "new"}
		if len(it.Path) > 0 {
			c.Section = it.Path[0]
		}
		if len(it.Path) > 1 {
			c.Chapter = it.Path[1]
		}
		for _, rp := range it.RubricPoints {
			c.Rubric = append(c.Rubric, Point{Content: rp.Content, Score: rp.Score, Keywords: rp.Keywords})
		}
		if e, ok := existing[it.Name]; ok {
			c.ExistingID, c.ExistingText = e.ID, e.OriginalText.String
			c.Alignment = "differ"
			if strings.TrimSpace(e.OriginalText.String) == strings.TrimSpace(it.OriginalText) {
				c.Alignment = "supplement"
			}
		}
		res = append(res, c)
	}
	return res, nil
}

// RubricCandidates 用 AI 从参考答案提采分点（编辑录题时用）。
func (s *Service) RubricCandidates(ctx context.Context, qtype, stem, answer string, score float64) ([]Point, error) {
	out, _, err := ai.ExtractRubric.Run(ctx, s.d.AI, 0, ai.RubricIn{QType: qtype, Stem: stem, Answer: answer, Score: score})
	if err != nil {
		return nil, apperr.New(apperr.AIFailed, "AI 提采分点失败，请稍后再试").Wrap(err)
	}
	pts := make([]Point, len(out.Points))
	for i, p := range out.Points {
		pts[i] = Point{Content: p.Content, Score: p.Score, Keywords: p.Keywords}
	}
	return pts, nil
}

// Draft 是一条官方内容草稿。
type Draft struct {
	ID           uint64
	ProjectID    uint64
	EditorID     uint64
	EntityType   string
	EntityID     uint64
	ChangeType   string
	Payload      json.RawMessage
	Status       string
	RejectReason string
	SubmittedAt  *time.Time
	CreatedAt    time.Time
}

func toDraft(d dbq.OfficialDraft) Draft {
	out := Draft{ID: d.ID, ProjectID: d.ProjectID, EditorID: d.EditorID, EntityType: string(d.EntityType), EntityID: uint64(d.EntityID.Int64), ChangeType: string(d.ChangeType),
		Payload: d.Payload, Status: string(d.Status), RejectReason: d.RejectReason.String, CreatedAt: d.CreatedAt}
	if d.SubmittedAt.Valid {
		t := d.SubmittedAt.Time
		out.SubmittedAt = &t
	}
	return out
}

func (s *Service) Drafts(ctx context.Context, projectID uint64, status string, editorID uint64) ([]Draft, error) {
	rows, err := s.q.ListOfficialDrafts(ctx, dbq.ListOfficialDraftsParams{ProjectID: projectID, Status: status, EditorID: int64(editorID)})
	if err != nil {
		return nil, err
	}
	out := make([]Draft, len(rows))
	for i, r := range rows {
		out[i] = toDraft(r)
	}
	return out, nil
}

// validateDraft 检查草稿：新增要有完整内容；修订、下线要指向这个项目里已有的官方条目。合并、拆分暂不支持（D40）。
func (s *Service) validateDraft(ctx context.Context, bankID uint64, entityType, changeType string, entityID uint64, payload json.RawMessage) error {
	switch changeType {
	case "add", "revise", "offline":
	default:
		return apperr.New(apperr.BadRequest, "暂只支持新增、修订、下线")
	}
	if changeType != "add" {
		if entityID == 0 {
			return apperr.New(apperr.BadRequest, "修订或下线要选择已有条目")
		}
		ok, err := s.entityInBank(ctx, bankID, entityType, entityID)
		if err != nil {
			return err
		}
		if !ok {
			return apperr.New(apperr.BadRequest, "这个条目不在本题库里")
		}
	}
	if changeType == "offline" {
		if entityType != "question" {
			return apperr.New(apperr.BadRequest, "暂只支持下线题目")
		}
		return nil
	}
	switch entityType {
	case "knowledge_point":
		var p KPPayload
		if err := json.Unmarshal(payload, &p); err != nil || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Section) == "" || strings.TrimSpace(p.Chapter) == "" {
			return apperr.New(apperr.BadRequest, "知识点要有板块、章节、名称")
		}
		if len(p.Rubric) == 0 {
			return apperr.New(apperr.BadRequest, "知识点要有采分点")
		}
	case "question":
		var p QuestionPayload
		if err := json.Unmarshal(payload, &p); err != nil || strings.TrimSpace(p.Stem) == "" || !slices.Contains(ai.QTypes, p.QType) {
			return apperr.New(apperr.BadRequest, "题目要有题型和题干")
		}
		if ai.Subjective(p.QType) && len(p.Rubric) == 0 {
			return apperr.New(apperr.BadRequest, "主观题要有采分点")
		}
	default:
		return apperr.New(apperr.BadRequest, "暂只支持知识点和题目")
	}
	return nil
}

func (s *Service) entityInBank(ctx context.Context, bankID uint64, entityType string, id uint64) (bool, error) {
	if entityType == "knowledge_point" {
		kps, err := s.q.ListOfficialKPs(ctx, bankID)
		return slices.ContainsFunc(kps, func(k dbq.ListOfficialKPsRow) bool { return k.ID == id && k.Level == dbq.KnowledgePointsLevelPoint }), err
	}
	qs, err := s.q.ListOfficialQuestions(ctx, bankID)
	return slices.ContainsFunc(qs, func(x dbq.ListOfficialQuestionsRow) bool { return x.ID == id }), err
}

// SaveDraft 新建（id 为 0）或修改草稿；编辑只写草稿，被退回的改完回到草稿。
func (s *Service) SaveDraft(ctx context.Context, editorID, projectID, id uint64, entityType, changeType string, entityID uint64, payload json.RawMessage) (Draft, error) {
	p, err := s.q.GetOfficialProject(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Draft{}, err
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if err := s.validateDraft(ctx, uint64(p.BankID.Int64), entityType, changeType, entityID, payload); err != nil {
		return Draft{}, err
	}
	if id != 0 {
		if err := s.draftIn(ctx, projectID, id); err != nil {
			return Draft{}, err
		}
	}
	eid := sql.NullInt64{Int64: int64(entityID), Valid: entityID != 0}
	if id == 0 {
		nid, err := s.q.InsertOfficialDraft(ctx, dbq.InsertOfficialDraftParams{ProjectID: projectID, EditorID: editorID, EntityType: dbq.OfficialDraftsEntityType(entityType),
			EntityID: eid, ChangeType: dbq.OfficialDraftsChangeType(changeType), Payload: payload})
		if err != nil {
			return Draft{}, err
		}
		id = uint64(nid)
	} else {
		n, err := s.q.UpdateOfficialDraft(ctx, dbq.UpdateOfficialDraftParams{ChangeType: dbq.OfficialDraftsChangeType(changeType), EntityID: eid, Payload: payload, ID: id, EditorID: editorID})
		if err != nil {
			return Draft{}, err
		}
		if n == 0 {
			return Draft{}, apperr.New(apperr.Conflict, "只有自己的草稿或被退回的可以修改")
		}
	}
	d, err := s.q.GetOfficialDraft(ctx, id)
	return toDraft(d), err
}

// draftIn 确认草稿属于这个项目（路由里的项目决定了编辑的访问范围）。
func (s *Service) draftIn(ctx context.Context, projectID, id uint64) error {
	d, err := s.q.GetOfficialDraft(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && d.ProjectID != projectID) {
		return apperr.NotFoundErr()
	}
	return err
}

func (s *Service) DeleteDraft(ctx context.Context, editorID, projectID, id uint64) error {
	if err := s.draftIn(ctx, projectID, id); err != nil {
		return err
	}
	n, err := s.q.DeleteOfficialDraft(ctx, dbq.DeleteOfficialDraftParams{ID: id, EditorID: editorID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.New(apperr.Conflict, "只有自己的草稿可以删除")
	}
	return nil
}

// fullReviewFirst 与 sampleRate：新编辑前 3 次提交全量审核，之后按 30% 抽检（PRD 10.2 7.13）。
const (
	fullReviewFirst = 3
	sampleRate      = 30
)

func randPercent() int {
	n, _ := rand.Int(rand.Reader, big.NewInt(100))
	return int(n.Int64())
}

// Submit 提交审核：前 3 次全量进审核队列；之后 30% 抽检进队列，其余直接通过（记为 skipped）。
func (s *Service) Submit(ctx context.Context, editorID, projectID, draftID uint64) (string, error) {
	if err := s.draftIn(ctx, projectID, draftID); err != nil {
		return "", err
	}
	now := s.now().UTC()
	mode := ""
	err := store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		n, err := q.SubmitOfficialDraft(ctx, dbq.SubmitOfficialDraftParams{SubmittedAt: sql.NullTime{Time: now, Valid: true}, ID: draftID, EditorID: editorID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.New(apperr.Conflict, "只有自己的草稿或被退回的可以提交")
		}
		count, err := q.CountEditorReviews(ctx, editorID)
		if err != nil {
			return err
		}
		switch {
		case count < fullReviewFirst:
			mode = "full"
		case randPercent() < sampleRate:
			mode = "sampled"
		default:
			mode = "skipped"
		}
		if mode == "skipped" {
			if _, err := q.InsertReviewTask(ctx, dbq.InsertReviewTaskParams{DraftID: draftID, Mode: dbq.ReviewTasksModeSkipped, Decision: dbq.ReviewTasksDecisionApproved,
				DecidedAt: sql.NullTime{Time: now, Valid: true}}); err != nil {
				return err
			}
			return q.SetOfficialDraftStatus(ctx, dbq.SetOfficialDraftStatusParams{Status: dbq.OfficialDraftsStatusApproved, ID: draftID})
		}
		_, err = q.InsertReviewTask(ctx, dbq.InsertReviewTaskParams{DraftID: draftID, Mode: dbq.ReviewTasksMode(mode), Decision: dbq.ReviewTasksDecisionPending})
		return err
	})
	return mode, err
}

// ---------- 7.13 审核队列 ----------

// Review 是待审核的一条：草稿与修改前的内容（修订时对比用）。
type Review struct {
	ID         uint64
	DraftID    uint64
	ProjectID  uint64
	EditorID   uint64
	Mode       string
	EntityType string
	EntityID   uint64
	ChangeType string
	Payload    json.RawMessage
	Before     json.RawMessage
	CreatedAt  time.Time
}

func (s *Service) Reviews(ctx context.Context, projectID uint64) ([]Review, error) {
	rows, err := s.q.ListPendingReviews(ctx, dbq.ListPendingReviewsParams{ProjectID: int64(projectID)})
	if err != nil {
		return nil, err
	}
	out := make([]Review, len(rows))
	contentByProject := map[uint64]content{}
	for i, r := range rows {
		rv := Review{ID: r.ID, DraftID: r.DraftID, ProjectID: r.ProjectID, EditorID: r.EditorID, Mode: string(r.Mode), EntityType: string(r.EntityType),
			EntityID: uint64(r.EntityID.Int64), ChangeType: string(r.ChangeType), Payload: r.Payload, CreatedAt: r.CreatedAt}
		if r.EntityID.Valid {
			c, ok := contentByProject[r.ProjectID]
			if !ok {
				p, err := s.q.GetOfficialProject(ctx, r.ProjectID)
				if err != nil {
					return nil, err
				}
				if c, err = loadContent(ctx, s.q, uint64(p.BankID.Int64)); err != nil {
					return nil, err
				}
				contentByProject[r.ProjectID] = c
			}
			rv.Before = c.payloadOf(string(r.EntityType), uint64(r.EntityID.Int64))
		}
		out[i] = rv
	}
	return out, nil
}

// payloadOf 把官方现有条目转成草稿格式（审核对比、回滚快照用）。
func (c content) payloadOf(entityType string, id uint64) json.RawMessage {
	pts := func(rs []dbq.ListOfficialRubricRow) []Point {
		out := make([]Point, len(rs))
		for i, r := range rs {
			s, _ := strconv.ParseFloat(r.Score.String, 64)
			var kw []string
			_ = json.Unmarshal(r.Keywords, &kw)
			out[i] = Point{Content: r.Content, Score: s, Keywords: kw}
		}
		return out
	}
	if entityType == "knowledge_point" {
		names := map[uint64]dbq.ListOfficialKPsRow{}
		for _, k := range c.kps {
			names[k.ID] = k
		}
		k, ok := names[id]
		if !ok {
			return nil
		}
		p := KPPayload{Name: k.Name, OriginalText: k.OriginalText.String, Rubric: pts(c.kpRubric[k.ID])}
		if ch, ok := names[uint64(k.ParentID.Int64)]; ok {
			p.Chapter = ch.Name
			if sec, ok := names[uint64(ch.ParentID.Int64)]; ok {
				p.Section = sec.Name
			}
		}
		b, _ := json.Marshal(p)
		return b
	}
	for _, x := range c.questions {
		if x.ID != id {
			continue
		}
		p := QuestionPayload{QType: string(x.Qtype), Stem: x.Stem, Answer: x.Answer.String, Analysis: x.Analysis.String, Rubric: pts(c.qRubric[x.ID]), KPNames: []string{}}
		_ = json.Unmarshal(x.Options, &p.Options)
		if x.Score.Valid {
			v, _ := strconv.ParseFloat(x.Score.String, 64)
			p.Score = &v
		}
		if x.ExamYear.Valid {
			v := int(x.ExamYear.Int16)
			p.ExamYear = &v
		}
		for _, l := range c.qKPs[x.ID] {
			for _, k := range c.kps {
				if k.ID == l.KpID {
					p.KPNames = append(p.KPNames, k.Name)
				}
			}
		}
		b, _ := json.Marshal(p)
		return b
	}
	return nil
}

// Decide 审核：通过、退回（必须填原因）、直接修改后通过。
func (s *Service) Decide(ctx context.Context, reviewerID, taskID uint64, decision, reason string, edited json.RawMessage) error {
	t, err := s.q.GetReviewTask(ctx, taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.NotFoundErr()
	}
	if err != nil {
		return err
	}
	if t.EditorID == reviewerID {
		return apperr.New(apperr.Forbidden, "不能审核自己提交的内容")
	}
	draftStatus := dbq.OfficialDraftsStatusApproved
	switch decision {
	case "approved":
	case "rejected":
		if strings.TrimSpace(reason) == "" {
			return apperr.New(apperr.BadRequest, "退回必须填原因")
		}
		draftStatus = dbq.OfficialDraftsStatusRejected
	case "edited":
		p, err := s.q.GetOfficialProject(ctx, t.ProjectID)
		if err != nil {
			return err
		}
		if err := s.validateDraft(ctx, uint64(p.BankID.Int64), string(t.EntityType), string(t.ChangeType), uint64(t.EntityID.Int64), edited); err != nil {
			return err
		}
	default:
		return apperr.New(apperr.BadRequest, "未知的审核结论")
	}
	now := s.now().UTC()
	return store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		n, err := q.DecideReviewTask(ctx, dbq.DecideReviewTaskParams{ReviewerID: sql.NullInt64{Int64: int64(reviewerID), Valid: true}, Decision: dbq.ReviewTasksDecision(decision),
			Reason: nullStr(reason), EditedPayload: dbJSON(edited, decision == "edited"), DecidedAt: sql.NullTime{Time: now, Valid: true}, ID: taskID})
		if err != nil {
			return err
		}
		if n == 0 {
			return apperr.New(apperr.Conflict, "这条已经审核过了")
		}
		if decision == "edited" {
			if err := q.SetOfficialDraftPayload(ctx, dbq.SetOfficialDraftPayloadParams{Payload: edited, ID: t.DraftID}); err != nil {
				return err
			}
		}
		return q.SetOfficialDraftStatus(ctx, dbq.SetOfficialDraftStatusParams{Status: draftStatus, RejectReason: nullStr(reason), ID: t.DraftID})
	})
}

func dbJSON(b json.RawMessage, ok bool) []byte {
	if !ok || len(b) == 0 {
		return nil
	}
	return b
}

// ---------- 7.14 版本发布 ----------

// Change 是变更清单的一项。
type Change struct {
	DraftID       uint64 `json:"draft_id"`
	EntityType    string `json:"entity_type"`
	ChangeType    string `json:"change_type"`
	EntityID      uint64 `json:"entity_id"`
	Title         string `json:"title"`
	RubricChanged bool   `json:"rubric_changed"`
}

// Check 是发布前的一项自动检查。
type Check struct {
	Name   string
	Passed bool
	Detail string
}

// Preview 是发布预览：自上个版本以来已审核的变更、自动检查、建议版本号与用户通知预览。
type Preview struct {
	Changes     []Change
	Checks      []Check
	NextVersion string
	Notice      string
	Subscribers int
}

func titleOf(entityType string, payload json.RawMessage) string {
	if entityType == "knowledge_point" {
		var p KPPayload
		_ = json.Unmarshal(payload, &p)
		return p.Name
	}
	var p QuestionPayload
	_ = json.Unmarshal(payload, &p)
	r := []rune(p.Stem)
	if len(r) > 40 {
		r = append(r[:40], '…')
	}
	return string(r)
}

func rubricOfPayload(entityType string, payload json.RawMessage) string {
	var pts []Point
	if entityType == "knowledge_point" {
		var p KPPayload
		_ = json.Unmarshal(payload, &p)
		pts = p.Rubric
	} else {
		var p QuestionPayload
		_ = json.Unmarshal(payload, &p)
		pts = p.Rubric
	}
	cs := make([]string, len(pts))
	for i, p := range pts {
		cs[i] = p.Content
	}
	return rubricText(cs)
}

var reVersion = regexp.MustCompile(`^\d+\.\d+$`)

func nextVersion(cur string) string {
	if !reVersion.MatchString(cur) {
		return "1.0"
	}
	parts := strings.Split(cur, ".")
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	return fmt.Sprintf("%d.%d", major, minor+1)
}

func (s *Service) ReleasePreview(ctx context.Context, projectID uint64) (Preview, error) {
	p, err := s.Project(ctx, projectID)
	if err != nil {
		return Preview{}, err
	}
	drafts, err := s.q.ListApprovedDrafts(ctx, projectID)
	if err != nil {
		return Preview{}, err
	}
	c, err := loadContent(ctx, s.q, p.BankID)
	if err != nil {
		return Preview{}, err
	}
	out := Preview{NextVersion: nextVersion(p.Version), Subscribers: p.Subscribers}
	for _, d := range drafts {
		ch := Change{DraftID: d.ID, EntityType: string(d.EntityType), ChangeType: string(d.ChangeType), EntityID: uint64(d.EntityID.Int64), Title: titleOf(string(d.EntityType), d.Payload)}
		if d.ChangeType == dbq.OfficialDraftsChangeTypeRevise {
			ch.RubricChanged = rubricOfPayload(string(d.EntityType), d.Payload) != rubricOfPayload(string(d.EntityType), c.payloadOf(string(d.EntityType), uint64(d.EntityID.Int64)))
		}
		if ch.ChangeType == "offline" {
			ch.Title = titleOf("question", c.payloadOf("question", ch.EntityID))
		}
		out.Changes = append(out.Changes, ch)
	}
	out.Checks = checksFor(drafts)
	out.Notice = fmt.Sprintf("「%s」官方题库更新到 %s 版：%d 处变更；你修改过的内容不会被覆盖", p.SubjectName, out.NextVersion, len(out.Changes))
	return out, nil
}

// checksFor 是发布前的自动检查（发布时在应用后的结果上再查一次）：有变更、知识点都有采分点、主观题都有采分点、
// 题目关联的知识点都存在。模拟卷结构检查等官方模拟卷上线后再加（D40）。
func checksFor(drafts []dbq.OfficialDraft) []Check {
	kpNames := map[string]bool{}
	var noRubric []string
	for _, d := range drafts {
		if d.EntityType == dbq.OfficialDraftsEntityTypeKnowledgePoint && d.ChangeType != dbq.OfficialDraftsChangeTypeOffline {
			var p KPPayload
			_ = json.Unmarshal(d.Payload, &p)
			kpNames[p.Name] = true
			if len(p.Rubric) == 0 {
				noRubric = append(noRubric, p.Name)
			}
		}
	}
	checks := []Check{{Name: "有已审核的变更", Passed: len(drafts) > 0}}
	checks = append(checks, Check{Name: "所有知识点都有采分点", Passed: len(noRubric) == 0, Detail: strings.Join(noRubric, "、")})
	return checks
}

// Version 是一个已发布的版本。
type Version struct {
	ID           uint64
	Version      string
	Changes      []Change
	PublishedAt  time.Time
	RolledBackAt *time.Time
}

func (s *Service) Versions(ctx context.Context, projectID uint64) ([]Version, error) {
	rows, err := s.q.ListOfficialVersions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]Version, len(rows))
	for i, r := range rows {
		v := Version{ID: r.ID, Version: r.Version, PublishedAt: r.PublishedAt, Changes: []Change{}}
		_ = json.Unmarshal(r.Changelog, &v.Changes)
		if r.RolledBackAt.Valid {
			t := r.RolledBackAt.Time
			v.RolledBackAt = &t
		}
		out[i] = v
	}
	return out, nil
}

// snapshotItem 是回滚用的「发布前」状态：新增的记下 ID（回滚时删除或下线），修订、下线的记下原内容。
type snapshotItem struct {
	EntityType string          `json:"entity_type"`
	ChangeType string          `json:"change_type"`
	ID         uint64          `json:"id"`
	Before     json.RawMessage `json:"before,omitempty"`
}

// Publish 发布版本：把已审核的变更应用到官方题库，检查，记版本与回滚快照，然后给订阅用户对账并通知。
// 首次上线时提醒自建了同一专业课的用户（PRD 5.6）。
func (s *Service) Publish(ctx context.Context, adminID, projectID uint64, version string) (Version, error) {
	p, err := s.Project(ctx, projectID)
	if err != nil {
		return Version{}, err
	}
	if version == "" {
		version = nextVersion(p.Version)
	}
	if !reVersion.MatchString(version) {
		return Version{}, apperr.New(apperr.BadRequest, "版本号格式如 1.2")
	}
	drafts, err := s.q.ListApprovedDrafts(ctx, projectID)
	if err != nil {
		return Version{}, err
	}
	for _, c := range checksFor(drafts) {
		if !c.Passed {
			return Version{}, apperr.New(apperr.Conflict, "发布前检查没通过："+c.Name+" "+c.Detail)
		}
	}
	firstRelease := p.Version == ""
	now := s.now().UTC()
	var vid int64
	var changes []Change
	err = store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		before, err := loadContent(ctx, q, p.BankID)
		if err != nil {
			return err
		}
		var snap []snapshotItem
		// 先应用知识点，题目关联知识点时才找得到。
		ordered := append([]dbq.OfficialDraft{}, drafts...)
		slices.SortStableFunc(ordered, func(a, b dbq.OfficialDraft) int {
			ka, kb := a.EntityType == dbq.OfficialDraftsEntityTypeKnowledgePoint, b.EntityType == dbq.OfficialDraftsEntityTypeKnowledgePoint
			switch {
			case ka && !kb:
				return -1
			case !ka && kb:
				return 1
			}
			return 0
		})
		for _, d := range ordered {
			ch := Change{DraftID: d.ID, EntityType: string(d.EntityType), ChangeType: string(d.ChangeType), EntityID: uint64(d.EntityID.Int64), Title: titleOf(string(d.EntityType), d.Payload)}
			item := snapshotItem{EntityType: string(d.EntityType), ChangeType: string(d.ChangeType), ID: uint64(d.EntityID.Int64)}
			if d.ChangeType != dbq.OfficialDraftsChangeTypeAdd {
				item.Before = before.payloadOf(string(d.EntityType), uint64(d.EntityID.Int64))
				if d.ChangeType == dbq.OfficialDraftsChangeTypeRevise {
					ch.RubricChanged = rubricOfPayload(string(d.EntityType), d.Payload) != rubricOfPayload(string(d.EntityType), item.Before)
				} else {
					ch.Title = titleOf("question", item.Before)
				}
			}
			id, err := s.apply(ctx, q, p.BankID, string(d.EntityType), string(d.ChangeType), uint64(d.EntityID.Int64), d.Payload)
			if err != nil {
				return fmt.Errorf("应用草稿 %d：%w", d.ID, err)
			}
			if d.ChangeType == dbq.OfficialDraftsChangeTypeAdd {
				item.ID, ch.EntityID = id, id
			}
			snap = append(snap, item)
			changes = append(changes, ch)
			if err := q.SetOfficialDraftStatus(ctx, dbq.SetOfficialDraftStatusParams{Status: dbq.OfficialDraftsStatusPublished, ID: d.ID}); err != nil {
				return err
			}
		}
		// 应用后的结果再检查一次：所有知识点都有采分点、没有空的板块与章节。
		after, err := loadContent(ctx, q, p.BankID)
		if err != nil {
			return err
		}
		if msg := after.problems(); msg != "" {
			return apperr.New(apperr.Conflict, "发布前检查没通过："+msg)
		}
		cl, _ := json.Marshal(nonNilChanges(changes))
		sn, _ := json.Marshal(snap)
		vid, err = q.InsertOfficialVersion(ctx, dbq.InsertOfficialVersionParams{ProjectID: projectID, Version: version, Changelog: cl, Snapshot: sn, PublishedBy: adminID, PublishedAt: now})
		if isDuplicate(err) {
			return apperr.New(apperr.Conflict, "这个版本号已经用过了")
		}
		if err != nil {
			return err
		}
		return q.SetOfficialProjectStage(ctx, dbq.SetOfficialProjectStageParams{Stage: dbq.OfficialProjectsStagePublished, ID: projectID})
	})
	if err != nil {
		return Version{}, err
	}
	if _, err := s.SyncBank(ctx, p.BankID); err != nil {
		return Version{}, err
	}
	s.notifyRelease(ctx, p, version, len(changes), firstRelease)
	return Version{ID: uint64(vid), Version: version, Changes: nonNilChanges(changes), PublishedAt: now}, nil
}

func nonNilChanges(cs []Change) []Change {
	if cs == nil {
		return []Change{}
	}
	return cs
}

// problems 检查官方题库的结构：知识点都有采分点、板块与章节下面都有内容（无孤立节点）。
func (c content) problems() string {
	children := map[uint64]int{}
	for _, k := range c.kps {
		if k.ParentID.Valid {
			children[uint64(k.ParentID.Int64)]++
		}
	}
	var noRubric, empty []string
	for _, k := range c.kps {
		if k.Level == dbq.KnowledgePointsLevelPoint && len(c.kpRubric[k.ID]) == 0 {
			noRubric = append(noRubric, k.Name)
		}
		if k.Level != dbq.KnowledgePointsLevelPoint && children[k.ID] == 0 {
			empty = append(empty, k.Name)
		}
	}
	var msgs []string
	if len(noRubric) > 0 {
		msgs = append(msgs, "这些知识点没有采分点："+strings.Join(noRubric, "、"))
	}
	if len(empty) > 0 {
		msgs = append(msgs, "这些板块或章节是空的："+strings.Join(empty, "、"))
	}
	return strings.Join(msgs, "；")
}

// notifyRelease 通知只发给添加了这个官方题库的用户；首次上线另提醒自建了同一专业课的用户。
func (s *Service) notifyRelease(ctx context.Context, p Project, version string, changes int, first bool) {
	subs, err := s.q.ListSubscribers(ctx, p.BankID)
	if err == nil {
		for _, sb := range subs {
			_ = s.q.InsertMessage(ctx, dbq.InsertMessageParams{OwnerUserID: sb.OwnerUserID, Mtype: dbq.MessagesMtypeOfficialBank, Title: "官方题库更新到 " + version,
				Body:      fmt.Sprintf("「%s」官方题库有 %d 处更新，新增的知识点标了「新」；你修改过的内容不会被覆盖", p.SubjectName, changes),
				Link:      notify.Link("bank", map[string]any{"subject_id": sb.SubjectID}),
				DedupeKey: sql.NullString{String: "official_release:" + strconv.FormatUint(p.BankID, 10) + ":" + version, Valid: true}})
		}
	}
	if first {
		_, _ = s.q.NotifyOfficialLaunch(ctx, dbq.NotifyOfficialLaunchParams{Title: "官方题库上线：" + p.School + " " + p.SubjectCode,
			Body: fmt.Sprintf("「%s %s」官方题库已上线，可以在题库页添加，和你自建的题库一起练", p.School, p.SubjectName), Link: notify.Link("official_banks", nil),
			DedupeKey: sql.NullString{String: "official_launch:" + strconv.FormatUint(p.BankID, 10), Valid: true}, Code: nullStr(p.SubjectCode)})
	}
}

// Rollback 回滚最近一个版本：按快照恢复发布前的内容（新增的知识点删除、新增的题目下线、修订的恢复原内容、下线的恢复），
// 然后给订阅用户对账。只能回滚最新的、还没回滚过的版本。
func (s *Service) Rollback(ctx context.Context, projectID, versionID uint64) error {
	p, err := s.Project(ctx, projectID)
	if err != nil {
		return err
	}
	vs, err := s.q.ListOfficialVersions(ctx, projectID)
	if err != nil {
		return err
	}
	var latest *dbq.ListOfficialVersionsRow
	for i := range vs {
		if !vs[i].RolledBackAt.Valid {
			latest = &vs[i]
			break
		}
	}
	if latest == nil || latest.ID != versionID {
		return apperr.New(apperr.Conflict, "只能回滚最新的版本")
	}
	v, err := s.q.GetOfficialVersion(ctx, dbq.GetOfficialVersionParams{ID: versionID, ProjectID: projectID})
	if err != nil {
		return err
	}
	var snap []snapshotItem
	_ = json.Unmarshal(v.Snapshot, &snap)
	now := s.now().UTC()
	err = store.WithTx(ctx, s.d.DB, func(q *dbq.Queries) error {
		for i := len(snap) - 1; i >= 0; i-- {
			it := snap[i]
			switch {
			case it.ChangeType == "add" && it.EntityType == "knowledge_point":
				if err := q.DeleteOfficialKP(ctx, dbq.DeleteOfficialKPParams{ID: it.ID, BankID: p.BankID}); err != nil {
					return err
				}
			case it.ChangeType == "add":
				if err := q.SetOfficialQuestionStatus(ctx, dbq.SetOfficialQuestionStatusParams{Status: dbq.QuestionsStatusOffline, ID: it.ID, BankID: p.BankID}); err != nil {
					return err
				}
			case it.ChangeType == "offline":
				if err := q.SetOfficialQuestionStatus(ctx, dbq.SetOfficialQuestionStatusParams{Status: dbq.QuestionsStatusActive, ID: it.ID, BankID: p.BankID}); err != nil {
					return err
				}
			default:
				if _, err := s.apply(ctx, q, p.BankID, it.EntityType, "revise", it.ID, it.Before); err != nil {
					return err
				}
			}
		}
		// 回滚后删掉空的板块与章节。
		after, err := loadContent(ctx, q, p.BankID)
		if err != nil {
			return err
		}
		if err := after.dropEmptyParents(ctx, q, p.BankID); err != nil {
			return err
		}
		_, err = q.MarkOfficialVersionRolledBack(ctx, dbq.MarkOfficialVersionRolledBackParams{RolledBackAt: sql.NullTime{Time: now, Valid: true}, ID: versionID})
		return err
	})
	if err != nil {
		return err
	}
	_, err = s.SyncBank(ctx, p.BankID)
	return err
}

func (c content) dropEmptyParents(ctx context.Context, q *dbq.Queries, bankID uint64) error {
	for range 2 { // 章节删完后板块可能也空了
		children := map[uint64]int{}
		for _, k := range c.kps {
			if k.ParentID.Valid {
				children[uint64(k.ParentID.Int64)]++
			}
		}
		var keep []dbq.ListOfficialKPsRow
		for _, k := range c.kps {
			if k.Level != dbq.KnowledgePointsLevelPoint && children[k.ID] == 0 {
				if err := q.DeleteOfficialKP(ctx, dbq.DeleteOfficialKPParams{ID: k.ID, BankID: bankID}); err != nil {
					return err
				}
				continue
			}
			keep = append(keep, k)
		}
		c.kps = keep
	}
	return nil
}

// apply 把一条变更写进官方题库，返回新增条目的 ID。
func (s *Service) apply(ctx context.Context, q *dbq.Queries, bankID uint64, entityType, changeType string, entityID uint64, payload json.RawMessage) (uint64, error) {
	if changeType == "offline" {
		return entityID, q.SetOfficialQuestionStatus(ctx, dbq.SetOfficialQuestionStatusParams{Status: dbq.QuestionsStatusOffline, ID: entityID, BankID: bankID})
	}
	if entityType == "knowledge_point" {
		var p KPPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return 0, err
		}
		kps, err := q.ListOfficialKPs(ctx, bankID)
		if err != nil {
			return 0, err
		}
		parent, err := ensurePath(ctx, q, bankID, kps, p.Section, p.Chapter)
		if err != nil {
			return 0, err
		}
		id := entityID
		if changeType == "add" {
			nid, err := q.InsertOfficialKP(ctx, dbq.InsertOfficialKPParams{BankID: bankID, ParentID: sql.NullInt64{Int64: int64(parent), Valid: true},
				Level: dbq.KnowledgePointsLevelPoint, Name: p.Name, OriginalText: nullStr(p.OriginalText), SortOrder: uint32(len(kps))})
			if err != nil {
				return 0, err
			}
			id = uint64(nid)
		} else {
			if err := q.UpdateOfficialKP(ctx, dbq.UpdateOfficialKPParams{Name: p.Name, OriginalText: nullStr(p.OriginalText), ID: id, BankID: bankID}); err != nil {
				return 0, err
			}
			if err := q.DeleteOfficialKPRubric(ctx, sql.NullInt64{Int64: int64(id), Valid: true}); err != nil {
				return 0, err
			}
		}
		return id, insertPoints(ctx, q, sql.NullInt64{}, sql.NullInt64{Int64: int64(id), Valid: true}, p.Rubric)
	}
	var p QuestionPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return 0, err
	}
	var opts []byte
	if len(p.Options) > 0 {
		opts, _ = json.Marshal(p.Options)
	}
	year := sql.NullInt16{}
	if p.ExamYear != nil {
		year = sql.NullInt16{Int16: int16(*p.ExamYear), Valid: true}
	}
	id := entityID
	if changeType == "add" {
		nid, err := q.InsertOfficialQuestion(ctx, dbq.InsertOfficialQuestionParams{BankID: bankID, Qtype: dbq.QuestionsQtype(p.QType), Stem: p.Stem, Options: opts,
			Answer: nullStr(p.Answer), Analysis: nullStr(p.Analysis), Score: scoreStr(p.Score), ExamYear: year, ContentHash: importer.ContentHash(p.Stem)})
		if err != nil {
			return 0, err
		}
		id = uint64(nid)
	} else {
		if err := q.UpdateOfficialQuestion(ctx, dbq.UpdateOfficialQuestionParams{Qtype: dbq.QuestionsQtype(p.QType), Stem: p.Stem, Options: opts, Answer: nullStr(p.Answer),
			Analysis: nullStr(p.Analysis), Score: scoreStr(p.Score), ExamYear: year, ContentHash: importer.ContentHash(p.Stem), ID: id, BankID: bankID}); err != nil {
			return 0, err
		}
		if err := q.DeleteOfficialQuestionRubric(ctx, sql.NullInt64{Int64: int64(id), Valid: true}); err != nil {
			return 0, err
		}
		if err := q.DeleteOfficialQuestionKPs(ctx, id); err != nil {
			return 0, err
		}
		if err := q.SetOfficialQuestionStatus(ctx, dbq.SetOfficialQuestionStatusParams{Status: dbq.QuestionsStatusActive, ID: id, BankID: bankID}); err != nil {
			return 0, err
		}
	}
	if err := insertPoints(ctx, q, sql.NullInt64{Int64: int64(id), Valid: true}, sql.NullInt64{}, p.Rubric); err != nil {
		return 0, err
	}
	kps, err := q.ListOfficialKPs(ctx, bankID)
	if err != nil {
		return 0, err
	}
	for i, name := range p.KPNames {
		for _, k := range kps {
			if k.Level == dbq.KnowledgePointsLevelPoint && k.Name == name {
				if err := q.InsertOfficialQuestionKP(ctx, dbq.InsertOfficialQuestionKPParams{QuestionID: id, KpID: k.ID, IsPrimary: i == 0}); err != nil {
					return 0, err
				}
				break
			}
		}
	}
	return id, nil
}

func insertPoints(ctx context.Context, q *dbq.Queries, questionID, kpID sql.NullInt64, pts []Point) error {
	for i, pt := range pts {
		sc := pt.Score
		if err := q.InsertOfficialRubricPoint(ctx, dbq.InsertOfficialRubricPointParams{QuestionID: questionID, KpID: kpID, Seq: uint16(i + 1), Content: pt.Content,
			Keywords: pointsJSON(pt), Score: scoreStr(&sc)}); err != nil {
			return err
		}
	}
	return nil
}

// ensurePath 按名称找或建「板块 → 章节」，返回章节 ID。
func ensurePath(ctx context.Context, q *dbq.Queries, bankID uint64, kps []dbq.ListOfficialKPsRow, section, chapter string) (uint64, error) {
	find := func(level dbq.KnowledgePointsLevel, name string, parent sql.NullInt64) (uint64, bool) {
		for _, k := range kps {
			if k.Level == level && k.Name == name && k.ParentID == parent {
				return k.ID, true
			}
		}
		return 0, false
	}
	sec, ok := find(dbq.KnowledgePointsLevelSection, section, sql.NullInt64{})
	if !ok {
		id, err := q.InsertOfficialKP(ctx, dbq.InsertOfficialKPParams{BankID: bankID, Level: dbq.KnowledgePointsLevelSection, Name: section, SortOrder: uint32(len(kps))})
		if err != nil {
			return 0, err
		}
		sec = uint64(id)
	}
	parent := sql.NullInt64{Int64: int64(sec), Valid: true}
	ch, ok := find(dbq.KnowledgePointsLevelChapter, chapter, parent)
	if !ok {
		id, err := q.InsertOfficialKP(ctx, dbq.InsertOfficialKPParams{BankID: bankID, ParentID: parent, Level: dbq.KnowledgePointsLevelChapter, Name: chapter, SortOrder: uint32(len(kps))})
		if err != nil {
			return 0, err
		}
		ch = uint64(id)
	}
	return ch, nil
}
