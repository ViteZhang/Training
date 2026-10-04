// Package official 是官方题库（T30，PRD 10.2 7.10–7.14、11.15）：立项、内容生产、审核、版本发布与回滚，以及用户添加 / 移除。
//
// 官方内容存在 source = official 的题库里（owner_user_id 为空）。用户添加后，官方内容复制到用户自己那门课的题库，
// 用 official_kp_id / official_question_id 指回官方条目：刷题、批改、复习、数据隔离都按原来的「题库 → 知识点 → 题目」走，
// 不区分来源（11.15）。每次发布或回滚后按官方题库的当前内容对每个订阅用户做一次对账（Sync）：
//   - 新增的复制过去，知识点标「新」两周；
//   - 用户没改过的（副本内容的哈希等于上次同步时记下的 official_hash）跟着更新，id 不变，掌握度与作答记录保留；
//     采分点有实质变化时，对应背诵重新进入复习；
//   - 用户改过的不覆盖，发一条「官方内容已更新」；
//   - 下线的题目归档（status = offline），不再出题。
//
// 同名知识点合并：用户已有同名知识点时只记下对应关系，保留用户自己的表述和采分点。
package official

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
)

// Point 是一个采分点。
type Point struct {
	Content  string   `json:"content"`
	Score    float64  `json:"score"`
	Keywords []string `json:"keywords,omitempty"`
}

// KPPayload 是知识点草稿：板块 / 章节 / 知识点，原文表述与采分点。
type KPPayload struct {
	Section      string  `json:"section"`
	Chapter      string  `json:"chapter"`
	Name         string  `json:"name"`
	OriginalText string  `json:"original_text"`
	Rubric       []Point `json:"rubric"`
}

// QuestionPayload 是题目草稿。
type QuestionPayload struct {
	QType    string      `json:"qtype"`
	Stem     string      `json:"stem"`
	Options  []ai.Option `json:"options,omitempty"`
	Answer   string      `json:"answer"`
	Analysis string      `json:"analysis,omitempty"`
	Score    *float64    `json:"score,omitempty"`
	ExamYear *int        `json:"exam_year,omitempty"`
	KPNames  []string    `json:"kp_names"`
	Rubric   []Point     `json:"rubric"`
}

// Deps 是创建服务的依赖。
type Deps struct {
	DB  *sql.DB
	AI  *ai.Engine
	Log *slog.Logger
	Now func() time.Time
}

// Service 是官方题库。
type Service struct {
	d   Deps
	q   *dbq.Queries
	now func() time.Time
}

func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{d: d, q: dbq.New(d.DB), now: d.Now}
}

// newMarkDays 是新版本新增的知识点标「新」的天数。
const newMarkDays = 14

func hashOf(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// rubricText 把采分点拼成稳定的字符串（参与内容哈希，判断「采分点有实质变化」）。
func rubricText(contents []string) string { return strings.Join(contents, "\n") }

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func scoreStr(v *float64) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: strconv.FormatFloat(*v, 'f', 2, 64), Valid: true}
}

func pointsJSON(p Point) dbtypes.NullJSON {
	if len(p.Keywords) == 0 {
		return nil
	}
	b, _ := json.Marshal(p.Keywords)
	return b
}

// content 是一个官方题库的当前内容（发布、回滚、对账都从它出发）。
type content struct {
	kps       []dbq.ListOfficialKPsRow
	questions []dbq.ListOfficialQuestionsRow
	kpRubric  map[uint64][]dbq.ListOfficialRubricRow
	qRubric   map[uint64][]dbq.ListOfficialRubricRow
	qKPs      map[uint64][]dbq.ListOfficialQuestionKPsRow
}

func loadContent(ctx context.Context, q *dbq.Queries, bankID uint64) (content, error) {
	var c content
	var err error
	if c.kps, err = q.ListOfficialKPs(ctx, bankID); err != nil {
		return c, err
	}
	// 按层级（板块 → 章节 → 知识点）排，复制时父节点先建好。
	order := map[dbq.KnowledgePointsLevel]int{dbq.KnowledgePointsLevelSection: 0, dbq.KnowledgePointsLevelChapter: 1, dbq.KnowledgePointsLevelPoint: 2}
	sort.SliceStable(c.kps, func(i, j int) bool { return order[c.kps[i].Level] < order[c.kps[j].Level] })
	if c.questions, err = q.ListOfficialQuestions(ctx, bankID); err != nil {
		return c, err
	}
	rub, err := q.ListOfficialRubric(ctx, dbq.ListOfficialRubricParams{BankID: bankID})
	if err != nil {
		return c, err
	}
	c.kpRubric, c.qRubric = map[uint64][]dbq.ListOfficialRubricRow{}, map[uint64][]dbq.ListOfficialRubricRow{}
	for _, r := range rub {
		if r.QuestionID.Valid {
			c.qRubric[uint64(r.QuestionID.Int64)] = append(c.qRubric[uint64(r.QuestionID.Int64)], r)
		} else if r.KpID.Valid {
			c.kpRubric[uint64(r.KpID.Int64)] = append(c.kpRubric[uint64(r.KpID.Int64)], r)
		}
	}
	qk, err := q.ListOfficialQuestionKPs(ctx, bankID)
	if err != nil {
		return c, err
	}
	c.qKPs = map[uint64][]dbq.ListOfficialQuestionKPsRow{}
	for _, r := range qk {
		c.qKPs[r.QuestionID] = append(c.qKPs[r.QuestionID], r)
	}
	return c, nil
}

func contents(rs []dbq.ListOfficialRubricRow) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Content
	}
	return out
}

func (c content) kpHash(k dbq.ListOfficialKPsRow) string {
	return hashOf(k.Name, k.OriginalText.String, rubricText(contents(c.kpRubric[k.ID])))
}

func (c content) questionHash(x dbq.ListOfficialQuestionsRow) string {
	return hashOf(string(x.Qtype), x.Stem, x.Answer.String, rubricText(contents(c.qRubric[x.ID])))
}

// isDuplicate 判断唯一键冲突（MySQL 1062）。
func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}
