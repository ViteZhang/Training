package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/gen"
	"peetraining-server/internal/practice"
)

// T45 体验一道题、学科通用体验题与 AI 现场出题（PRD v3 增补）；后台 7.18。

func init() {
	flagGuards["POST "+APIPrefix+"/trials/recognize"] = "trial"
	flagGuards["GET "+APIPrefix+"/sample-questions"] = "sample_trial"
	flagGuards["POST "+APIPrefix+"/sample-questions/live"] = "ai_live_question"
	// POST /trials 按请求内容分别检查 trial / sample_trial / ai_live_question（在 practice.CreateTrial 里）。
}

func toGenTrial(t practice.Trial) gen.Trial {
	out := gen.Trial{Id: int64(t.ID), Kind: gen.TrialKind(t.Kind), Qtype: gen.TrialQtype(t.QType), Score: float32(t.Score), Stem: t.Stem,
		AnswerSource: gen.TrialAnswerSource(t.AnswerSource), PointCount: t.PointCount, Status: gen.TrialStatus(t.Status), Remaining: t.Remaining,
		QuestionId: optID(t.QuestionID)}
	if t.Status == "ready" {
		return out
	}
	out.ReferenceAnswer, out.AnswerText = optStr(t.Reference), optStr(t.AnswerText)
	if t.Total != nil {
		out.Total = ptr(float32(*t.Total))
	}
	pts := make([]gen.GradingPoint, len(t.Points))
	for i, p := range t.Points {
		pts[i] = gen.GradingPoint{Seq: p.Seq, Content: p.Content, Score: float32(p.Score), Got: float32(p.Got), Verdict: gen.GradingPointVerdict(p.Verdict),
			Quote: optStr(p.Quote), Reason: optStr(p.Reason)}
	}
	out.Points = &pts
	items := t.Extra.Items
	if items == nil {
		items = []string{}
	}
	out.Suggestions, out.StructureOk, out.StructureNote = &items, ptr(t.Extra.StructureOK), optStr(t.Extra.StructureNote)
	return out
}

func (h *Handlers) GetTrialStatus(c *gin.Context) {
	st, err := h.deps.Practice.TrialStatus(c.Request.Context(), currentUser(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gen.TrialUsage{Limit: st.Limit, Used: st.Used, Remaining: st.Remaining})
}

func (h *Handlers) RecognizeTrial(c *gin.Context) {
	var body gen.RecognizeTrialJSONBody
	if !bind(c, &body) {
		return
	}
	r, err := h.deps.Practice.RecognizeTrial(c.Request.Context(), currentUser(c), body.Text)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"qtype": r.QType, "score": r.Score, "stem": r.Stem, "answer": optStr(r.Answer), "supported": r.Supported})
}

func (h *Handlers) CreateTrial(c *gin.Context) {
	var body gen.CreateTrialJSONBody
	if !bind(c, &body) {
		return
	}
	in := practice.TrialInput{}
	if body.SampleQuestionId != nil {
		in.SampleQuestionID = uint64(*body.SampleQuestionId)
	} else {
		if body.Qtype == nil || body.Stem == nil || body.Score == nil {
			_ = c.Error(apperr.New(apperr.BadRequest, "题型、分值、题干都要填"))
			return
		}
		in.QType, in.Stem, in.Score = string(*body.Qtype), *body.Stem, float64(*body.Score)
		if body.ReferenceAnswer != nil {
			in.Answer = *body.ReferenceAnswer
		}
		if body.InputMode != nil {
			in.InputMode = string(*body.InputMode)
		}
	}
	t, err := h.deps.Practice.CreateTrial(c.Request.Context(), currentUser(c), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenTrial(t))
}

func (h *Handlers) GetTrial(c *gin.Context, trialID int64) {
	t, err := h.deps.Practice.Trial(c.Request.Context(), currentUser(c), uint64(trialID))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenTrial(t))
}

func (h *Handlers) SubmitTrial(c *gin.Context, trialID int64) {
	var body gen.SubmitTrialJSONBody
	if !bind(c, &body) {
		return
	}
	in := practice.TrialAnswer{Key: body.IdempotencyKey, Answer: body.Answer}
	if body.Mode != nil {
		in.Mode = string(*body.Mode)
	}
	if body.PhotoKeys != nil {
		in.PhotoKeys = *body.PhotoKeys
	}
	t, err := h.deps.Practice.SubmitTrial(c.Request.Context(), currentUser(c), uint64(trialID), in)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenTrial(t))
}

func (h *Handlers) SaveTrial(c *gin.Context, trialID int64) {
	var body gen.SaveTrialJSONBody
	if !bind(c, &body) {
		return
	}
	var subject uint64
	if body.SubjectId != nil {
		subject = uint64(*body.SubjectId)
	}
	if body.Action != "discard" && subject == 0 {
		_ = c.Error(apperr.New(apperr.BadRequest, "请选择加入哪门专业课"))
		return
	}
	qid, err := h.deps.Practice.SaveTrial(c.Request.Context(), currentUser(c), uint64(trialID), string(body.Action), subject)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"question_id": optID(qid)})
}

func toGenSample(s practice.SampleItem) gen.SampleQuestion {
	return gen.SampleQuestion{Id: int64(s.ID), Qtype: gen.SampleQuestionQtype(s.QType), Score: float32(s.Score), Stem: s.Stem, Textbook: optStr(s.Textbook), AiGenerated: s.AI}
}

func (h *Handlers) ListSampleQuestions(c *gin.Context) {
	ds, err := h.deps.Practice.SampleCatalog(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	type direction struct {
		ID        uint64               `json:"id"`
		Name      string               `json:"name"`
		Questions []gen.SampleQuestion `json:"questions"`
	}
	type discipline struct {
		Name       string      `json:"name"`
		Directions []direction `json:"directions"`
	}
	out := make([]discipline, len(ds))
	for i, d := range ds {
		out[i] = discipline{Name: d.Name, Directions: make([]direction, len(d.Directions))}
		for j, dir := range d.Directions {
			qs := make([]gen.SampleQuestion, len(dir.Questions))
			for k, q := range dir.Questions {
				qs[k] = toGenSample(q)
			}
			out[i].Directions[j] = direction{ID: dir.ID, Name: dir.Name, Questions: qs}
		}
	}
	c.JSON(http.StatusOK, items(out))
}

func (h *Handlers) LiveSampleQuestion(c *gin.Context) {
	var body gen.LiveSampleQuestionJSONBody
	if !bind(c, &body) {
		return
	}
	s, err := h.deps.Practice.LiveSample(c.Request.Context(), currentUser(c), uint64(body.SubjectId))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, toGenSample(s))
}

// --- 后台 7.18 ---

func (h *Handlers) AdminSampleOverview(c *gin.Context) {
	st, dirs, err := h.deps.Practice.SampleOverview(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	type dir struct {
		ID            uint64 `json:"id"`
		Discipline    string `json:"discipline"`
		Name          string `json:"name"`
		QuestionCount int    `json:"question_count"`
	}
	out := make([]dir, len(dirs))
	for i, d := range dirs {
		out[i] = dir{ID: d.ID, Discipline: d.Discipline, Name: d.Name, QuestionCount: d.QuestionCount}
	}
	c.JSON(http.StatusOK, gin.H{"disciplines": st.Disciplines, "directions": st.Directions, "questions": st.Questions, "pending": st.Pending,
		"week_tries": st.WeekTries, "items": out})
}

func (h *Handlers) AdminCreateSampleDirection(c *gin.Context) {
	var body gen.AdminCreateSampleDirectionJSONBody
	if !bind(c, &body) {
		return
	}
	id, err := h.deps.Practice.CreateSampleDirection(c.Request.Context(), body.Discipline, body.Name)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (h *Handlers) AdminListSampleQuestions(c *gin.Context, p gen.AdminListSampleQuestionsParams) {
	var dir uint64
	if p.DirectionId != nil {
		dir = uint64(*p.DirectionId)
	}
	rows, err := h.deps.Practice.SampleQuestions(c.Request.Context(), dir)
	if err != nil {
		_ = c.Error(err)
		return
	}
	out := make([]gen.AdminSampleQuestion, len(rows))
	for i, r := range rows {
		rub := make([]gen.RubricPointInput, len(r.Rubric))
		for j, p := range r.Rubric {
			kw := p.Keywords
			rub[j] = gen.RubricPointInput{Content: p.Content, Score: ptr(float32(p.Score)), Keywords: &kw}
		}
		out[i] = gen.AdminSampleQuestion{Id: int64(r.ID), DirectionId: optID(r.DirectionID), Origin: gen.AdminSampleQuestionOrigin(r.Origin),
			SubjectLabel: optStr(r.SubjectLabel), Textbook: optStr(r.Textbook), Qtype: gen.AdminSampleQuestionQtype(r.QType), Score: float32(r.Score),
			Stem: r.Stem, Answer: r.Answer, Rubric: rub, Status: gen.AdminSampleQuestionStatus(r.Status), TryCount: r.TryCount, CreatedAt: r.CreatedAt}
	}
	c.JSON(http.StatusOK, items(out))
}

func sampleInput(textbook *string, qtype string, score float32, stem, answer string, rubric []gen.RubricPointInput) practice.SampleInput {
	in := practice.SampleInput{QType: qtype, Score: float64(score), Stem: stem, Answer: answer}
	if textbook != nil {
		in.Textbook = *textbook
	}
	for _, p := range rubric {
		rp := ai.RubricPoint{Content: p.Content}
		if p.Score != nil {
			rp.Score = float64(*p.Score)
		}
		if p.Keywords != nil {
			rp.Keywords = *p.Keywords
		}
		in.Rubric = append(in.Rubric, rp)
	}
	return in
}

func (h *Handlers) AdminCreateSampleQuestion(c *gin.Context) {
	var body gen.AdminCreateSampleQuestionJSONBody
	if !bind(c, &body) {
		return
	}
	id, err := h.deps.Practice.SaveSample(c.Request.Context(), currentAdmin(c).ID, uint64(body.DirectionId), 0,
		sampleInput(body.Textbook, string(body.Qtype), body.Score, body.Stem, body.Answer, body.Rubric))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (h *Handlers) AdminUpdateSampleQuestion(c *gin.Context, sampleID int64) {
	var body gen.SampleQuestionInput
	if !bind(c, &body) {
		return
	}
	if _, err := h.deps.Practice.SaveSample(c.Request.Context(), currentAdmin(c).ID, 0, uint64(sampleID),
		sampleInput(body.Textbook, string(body.Qtype), body.Score, body.Stem, body.Answer, body.Rubric)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) AdminSetSampleStatus(c *gin.Context, sampleID int64) {
	var body gen.AdminSetSampleStatusJSONBody
	if !bind(c, &body) {
		return
	}
	if err := h.deps.Practice.SetSampleStatus(c.Request.Context(), currentAdmin(c).ID, uint64(sampleID), string(body.Status)); err != nil {
		_ = c.Error(err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handlers) AdminGenerateSamples(c *gin.Context) {
	var body gen.AdminGenerateSamplesJSONBody
	if !bind(c, &body) {
		return
	}
	book := ""
	if body.Textbook != nil {
		book = *body.Textbook
	}
	n, err := h.deps.Practice.GenerateSamples(c.Request.Context(), currentAdmin(c).ID, uint64(body.DirectionId), string(body.Qtype), float64(body.Score), body.Count, book)
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"created": n})
}
