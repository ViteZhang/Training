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
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/dbtypes"
	"peetraining-server/internal/params"
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
	// Reference 是出处（T56）：表格导入填写，发布后展示给用户。
	Reference string `json:"reference,omitempty"`
}

// QuestionPayload 是题目草稿。
type QuestionPayload struct {
	AnswerSnapshotID uint64      `json:"answer_snapshot_id,omitempty"`
	QType            string      `json:"qtype"`
	Stem             string      `json:"stem"`
	Options          []ai.Option `json:"options,omitempty"`
	Answer           string      `json:"answer"`
	Analysis         string      `json:"analysis,omitempty"`
	// Reference 是出处（T56）：表格导入填写，发布后展示给用户（不管答案是不是 AI 生成的）。
	Reference string   `json:"reference,omitempty"`
	Score     *float64 `json:"score,omitempty"`
	ExamYear  *int     `json:"exam_year,omitempty"`
	KPNames   []string `json:"kp_names"`
	Rubric    []Point  `json:"rubric"`
	// AnswerAI 表示参考答案由 AI 生成（T49）：经编辑审核发布后，用户端显示「AI 参考答案」，照常计入预估分。
	AnswerAI bool `json:"answer_ai,omitempty"`
	// ScoreInferred 表示分值是补答案时参考同项目同题型的给分填上的（原文没有分值），请编辑核对。
	ScoreInferred bool `json:"score_inferred,omitempty"`
	// 以下三项只在 7.17 核对区用，接受时去掉、不进草稿（T53）：
	// AnswerBasis 是 AI 生成的答案没有引用教材的原因；RubricKept 是采分点是原文给的（重新生成时保留）；
	// Regenerate 是编辑点了「按关联教材重新生成」、还没生成完。
	AnswerBasis *ai.AnswerBasis `json:"answer_basis,omitempty"`
	RubricKept  bool            `json:"rubric_kept,omitempty"`
	Regenerate  bool            `json:"regenerate,omitempty"`
	// FillQueuedAt 是编辑点了单题「生成答案 / 重新生成」、Worker 还没生成完的时间（核对区显示「生成中」），同样不进草稿。
	FillQueuedAt *time.Time `json:"fill_queued_at,omitempty"`
	// Group 不为空是限选题（T51）：按专业分组的题（「第一组：请选择文艺学专业的考生作答」，回答非报考专业不得分），
	// 只有报考这些专业的考生要答；为空是必答题。
	Group *QuestionGroup `json:"group,omitempty"`
}

// QuestionGroup 是限选题所在的组。
type QuestionGroup struct {
	Label  string `json:"label"`            // 如「第一组」
	Majors string `json:"majors,omitempty"` // 这一组适用的专业，如「文艺学、比较文学与世界文学」
}

// groupCols 是写进 questions.group_label / group_majors 的值；必答题都为空。
func groupCols(g *QuestionGroup) (sql.NullString, sql.NullString) {
	if g == nil || strings.TrimSpace(g.Label) == "" {
		return sql.NullString{}, sql.NullString{}
	}
	return nullStr(strings.TrimSpace(g.Label)), nullStr(strings.TrimSpace(g.Majors))
}

// groupOf 从 questions 的两列还原分组。
func groupOf(label, majors sql.NullString) *QuestionGroup {
	if label.String == "" {
		return nil
	}
	return &QuestionGroup{Label: label.String, Majors: majors.String}
}

// groupKey 计入内容哈希：限选题的组变了，副本跟着更新；必答题为空串，哈希与以前一致。
func groupKey(label, majors sql.NullString) string {
	if label.String == "" {
		return ""
	}
	return label.String + "\x00" + majors.String
}

// answerOrigin 是官方题的答案来源：AI 生成的记 ai_generated，其余是团队给的 official。
func answerOrigin(p QuestionPayload) dbq.NullQuestionsAnswerOrigin {
	if p.AnswerAI && strings.TrimSpace(p.Answer) != "" {
		return dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: dbq.QuestionsAnswerOriginAiGenerated, Valid: true}
	}
	return dbq.NullQuestionsAnswerOrigin{QuestionsAnswerOrigin: dbq.QuestionsAnswerOriginOfficial, Valid: true}
}

// Deps 是创建服务的依赖。
type Deps struct {
	DB  *sql.DB
	AI  *ai.Engine
	Log *slog.Logger
	Now func() time.Time
	// Params 读申请收录的阈值（rule_params.official_first）；为空时用默认值。
	Params *params.Store
	// OpsWebhook 是运营企业微信群机器人地址（申请收录推送）；为空时只写日志。AdminBaseURL 是推送里的后台链接前缀。
	OpsWebhook   string
	AdminBaseURL string
	HTTP         *http.Client
}

// Service 是官方题库。
type Service struct {
	d       Deps
	imports ImportDeps
	q       *dbq.Queries
	now     func() time.Time
}

func New(d Deps) *Service {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.HTTP == nil {
		d.HTTP = &http.Client{Timeout: 5 * time.Second}
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
	return kpHashOf(k.Name, k.OriginalText.String, rubricText(contents(c.kpRubric[k.ID])), k.ReferenceText.String)
}

// kpHashOf 是知识点内容的哈希。出处有内容时才多算一项（T56），以前同步的副本哈希不变。
func kpHashOf(name, original, rubric, reference string) string {
	parts := []string{name, original, rubric}
	if reference != "" {
		parts = append(parts, "ref:"+reference)
	}
	return hashOf(parts...)
}

func (c content) questionHash(x dbq.ListOfficialQuestionsRow) string {
	return questionHashOf(string(x.Qtype), x.Stem, x.Answer.String, rubricText(contents(c.qRubric[x.ID])), x.AnswerOrigin, groupKey(x.GroupLabel, x.GroupMajors),
		x.Analysis.String, x.ReferenceText.String)
}

// questionHashOf 是题目内容的哈希。答案是 AI 生成的、限选题的分组才多算一项，团队给的答案、必答题的哈希与以前一致，
// 已同步的副本不会因此被判为有变化。
// 解析、出处（T56）也一样：有内容时才多算，只改了解析或出处的版本也能同步给已添加的用户。
func questionHashOf(qtype, stem, answer, rubric string, origin dbq.NullQuestionsAnswerOrigin, group, analysis, reference string) string {
	parts := []string{qtype, stem, answer, rubric}
	if origin.Valid && origin.QuestionsAnswerOrigin == dbq.QuestionsAnswerOriginAiGenerated {
		parts = append(parts, "ai_generated")
	}
	if group != "" {
		parts = append(parts, "group:"+group)
	}
	if analysis != "" {
		parts = append(parts, "analysis:"+analysis)
	}
	if reference != "" {
		parts = append(parts, "ref:"+reference)
	}
	return hashOf(parts...)
}

// isDuplicate 判断唯一键冲突（MySQL 1062）。
func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}
