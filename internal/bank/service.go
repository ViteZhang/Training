// Package bank 是题库（T13）：知识点树与卡片、题目、原文查看、搜索。用户看得到、管得了自己导入的内容（PRD 模块 3）。
package bank

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/params"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/rules"
)

// Service 是题库服务。
type Service struct {
	db     *sql.DB
	q      *dbq.Queries
	ai     *ai.Engine
	quota  *quota.Service
	params *params.Store
	now    func() time.Time
}

// Deps 是创建服务的依赖。
type Deps struct {
	DB     *sql.DB
	AI     *ai.Engine
	Quota  *quota.Service
	Params *params.Store
	Now    func() time.Time
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{db: d.DB, q: dbq.New(d.DB), ai: d.AI, quota: d.Quota, params: d.Params, now: now}
}

func owner(userID uint64) sql.NullInt64 { return sql.NullInt64{Int64: int64(userID), Valid: true} }

// subject 返回专业课与题库；不是自己的返回 404。
func (s *Service) subject(ctx context.Context, userID, subjectID uint64) (dbq.GetSubjectBankRow, error) {
	b, err := s.q.GetSubjectBank(ctx, dbq.GetSubjectBankParams{ID: subjectID, OwnerUserID: userID, OwnerUserID_2: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) {
		return b, apperr.NotFoundErr()
	}
	return b, err
}

// Overview 是题库概况（题库页头部、2.1「我的题库」卡）。
type Overview struct {
	SubjectID, BankID                              uint64
	Questions, KPs, NeedsReview, Materials, Papers int
	Unlearned, Learning, Consolidating, Mastered   int
	IsEssay                                        bool
}

func (s *Service) Overview(ctx context.Context, userID, subjectID uint64) (Overview, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return Overview{}, err
	}
	c, err := s.q.BankCounts(ctx, dbq.BankCountsParams{BankID: b.BankID, Owner: owner(userID), UserID: userID})
	if err != nil {
		return Overview{}, err
	}
	o := Overview{SubjectID: subjectID, BankID: b.BankID, Questions: int(c.QuestionCount), KPs: int(c.KpCount),
		NeedsReview: int(c.QuestionReviewCount + c.KpReviewCount), Materials: int(c.MaterialCount), Papers: int(c.PaperCount), IsEssay: b.IsEssay}
	kps, err := s.q.ListBankKPsFull(ctx, dbq.ListBankKPsFullParams{UserID: userID, BankID: b.BankID, Owner: owner(userID)})
	if err != nil {
		return Overview{}, err
	}
	for _, k := range kps {
		if k.Level != dbq.KnowledgePointsLevelPoint {
			continue
		}
		switch k.State {
		case dbq.KpMasteryStateLearning:
			o.Learning++
		case dbq.KpMasteryStateConsolidating:
			o.Consolidating++
		case dbq.KpMasteryStateMastered:
			o.Mastered++
		default:
			o.Unlearned++
		}
	}
	return o, nil
}

// Node 是知识点树的一个节点（3.1）。
type Node struct {
	ID            uint64
	Level         string
	Name          string
	KPCount       int     // 板块与章节：知识点数
	Consolidating int     // 板块与章节：待巩固数
	AvgMastery    float64 // 板块：平均掌握度
	IsWeak        bool    // 板块：掌握度低于 40% 标「短板」
	ExamCount     int     // 知识点：真题出现次数
	State         string
	M             float64
	NeedsReview   bool
	Official      bool // 官方题库复制过来的（同名合并的仍是用户自己的）
	IsNew         bool // 官方新版本新增的，标「新」两周（PRD 11.15）
	Children      []*Node
}

// Tree 返回知识点树。filter：all / unmastered（未掌握）/ exam（真题考过）/ needs_review（待核对）；
// 筛选只作用在知识点上，没有剩下知识点的板块与章节不返回。统计数始终按全部知识点算。
func (s *Service) Tree(ctx context.Context, userID, subjectID uint64, filter string) ([]*Node, int, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return nil, 0, err
	}
	kps, err := s.q.ListBankKPsFull(ctx, dbq.ListBankKPsFullParams{UserID: userID, BankID: b.BankID, Owner: owner(userID)})
	if err != nil {
		return nil, 0, err
	}
	p, err := s.params.Rules(ctx)
	if err != nil {
		return nil, 0, err
	}
	c, err := s.q.BankCounts(ctx, dbq.BankCountsParams{BankID: b.BankID, Owner: owner(userID), UserID: userID})
	if err != nil {
		return nil, 0, err
	}
	nodes := map[uint64]*Node{}
	for _, k := range kps {
		m, _ := strconv.ParseFloat(k.M, 64)
		nodes[k.ID] = &Node{ID: k.ID, Level: string(k.Level), Name: k.Name, ExamCount: int(k.ExamCount), State: string(k.State), M: m,
			NeedsReview: k.NeedsReview, Official: k.Origin == dbq.KnowledgePointsOriginOfficial,
			IsNew: k.OfficialNewUntil.Valid && k.OfficialNewUntil.Time.After(s.now())}
	}
	var roots []*Node
	for _, k := range kps {
		n := nodes[k.ID]
		if parent, ok := nodes[uint64(k.ParentID.Int64)]; k.ParentID.Valid && ok {
			parent.Children = append(parent.Children, n)
		} else {
			roots = append(roots, n)
		}
	}
	for _, r := range roots {
		stats(r, p.MasteryState)
	}
	keep := func(n *Node) bool {
		switch filter {
		case "unmastered":
			return n.State != string(dbq.KpMasteryStateMastered)
		case "exam":
			return n.ExamCount > 0
		case "needs_review":
			return n.NeedsReview
		}
		return true
	}
	if filter != "" && filter != "all" {
		roots = prune(roots, keep)
	}
	return roots, int(c.QuestionReviewCount + c.KpReviewCount), nil
}

// stats 统计节点下的知识点数、待巩固数，板块再算平均掌握度与是否短板（PRD 11.2）。返回子树里全部知识点的 M。
func stats(n *Node, p rules.MasteryStateParams) []float64 {
	if n.Level == string(dbq.KnowledgePointsLevelPoint) {
		ms := []float64{n.M}
		for _, c := range n.Children {
			ms = append(ms, stats(c, p)...)
		}
		return ms
	}
	var ms []float64
	for _, c := range n.Children {
		sub := stats(c, p)
		ms = append(ms, sub...)
		if c.Level == string(dbq.KnowledgePointsLevelPoint) {
			n.KPCount++
			if c.State == string(dbq.KpMasteryStateConsolidating) {
				n.Consolidating++
			}
		} else {
			n.KPCount += c.KPCount
			n.Consolidating += c.Consolidating
		}
	}
	if n.Level == string(dbq.KnowledgePointsLevelSection) {
		n.AvgMastery, n.IsWeak = rules.SectionMastery(ms, p)
	}
	return ms
}

func prune(nodes []*Node, keep func(*Node) bool) []*Node {
	var out []*Node
	for _, n := range nodes {
		n.Children = prune(n.Children, keep)
		if n.Level == string(dbq.KnowledgePointsLevelPoint) {
			if keep(n) || len(n.Children) > 0 {
				out = append(out, n)
			}
			continue
		}
		if len(n.Children) > 0 {
			out = append(out, n)
		}
	}
	return out
}
