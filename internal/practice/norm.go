package practice

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/quota"
)

// 4.13 答题规范（T19）。

// NormElement 是题型结构的一个要素与约占分值。
type NormElement struct {
	Name  string
	Desc  string
	Share float64
}

// normTypes 是各题型的答题结构（PRD 4.13：名词解释三段：定义、特征要点、出处或例子，并标约占分值）。
// 简答与论述的结构按常见阅卷要求整理，约占比例记在 open-questions D21，后续可移到 rule_params。
var normTypes = map[string]struct {
	Elements []NormElement
	Tips     []string
}{
	"term": {
		Elements: []NormElement{
			{"定义", "一句话说清是什么，用资料里的规范表述", 0.4},
			{"特征要点", "分点写出采分点里的关键特征", 0.4},
			{"出处或例子", "写明提出者、出处，或举一个作品例子", 0.2},
		},
		Tips: []string{"三段式，80–150 字", "关键词用资料原文的说法，不要自己换词", "例子点到为止，不展开"},
	},
	"short_answer": {
		Elements: []NormElement{
			{"总述", "开头一句点明核心观点或概念", 0.15},
			{"分点作答", "按采分点分条作答，每点先写要点再简单解释", 0.7},
			{"小结", "最后一句收束，回扣题目", 0.15},
		},
		Tips: []string{"300–500 字", "分点写，一点一段，要点放在每段开头", "采分点之外的发挥少写"},
	},
	"discussion": {
		Elements: []NormElement{
			{"观点", "开头亮明自己的观点，回应题目", 0.15},
			{"分论点与论证", "分几个层次论证，每层有论点、材料与分析", 0.7},
			{"结论", "总结全文，必要时提升到理论或现实意义", 0.15},
		},
		Tips: []string{"800 字以上", "分论点要覆盖资料里的采分点", "论证用作品和理论材料，不要空谈"},
	},
}

// NormExample 是高分写法：用户资料里的原文与采分点。
type NormExample struct {
	QuestionID   uint64
	Stem         string
	KPName       string
	OriginalText string
	Reference    string
	Points       []string
	MaterialID   uint64
	FileName     string
	Page         int
}

// NormLast 是你上次的写法：最近一次批改完的同题型作答，标出缺了什么。
type NormLast struct {
	QuestionID uint64
	Stem       string
	Answer     string
	GradingID  uint64
	Score      *float64
	FullScore  *float64
	Missing    []string
}

// AnswerNorm 是 4.13 页面数据。
type AnswerNorm struct {
	QType      string
	Elements   []NormElement
	Tips       []string
	Example    *NormExample
	Last       *NormLast
	PracticeID uint64
}

func (s *Service) AnswerNorm(ctx context.Context, userID, subjectID uint64, qtype string) (AnswerNorm, error) {
	t, ok := normTypes[qtype]
	if !ok {
		return AnswerNorm{}, apperr.NotFoundErr()
	}
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return AnswerNorm{}, err
	}
	out := AnswerNorm{QType: qtype, Elements: t.Elements, Tips: t.Tips}
	qt := dbq.QuestionsQtype(qtype)
	ex, err := s.q.NormExampleQuestion(ctx, dbq.NormExampleQuestionParams{BankID: b.BankID, OwnerUserID: owner(userID), Qtype: qt})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err == nil {
		e := &NormExample{QuestionID: ex.ID, Stem: ex.Stem, Reference: ex.Answer.String}
		rp, err := s.q.ListQuestionRubric(ctx, dbq.ListQuestionRubricParams{QuestionID: sql.NullInt64{Int64: int64(ex.ID), Valid: true}, OwnerUserID: owner(userID)})
		if err != nil {
			return out, err
		}
		for _, p := range rp {
			e.Points = append(e.Points, p.Content)
		}
		kps, err := s.q.ListQuestionKPs(ctx, dbq.ListQuestionKPsParams{QuestionID: ex.ID, OwnerUserID: owner(userID)})
		if err != nil {
			return out, err
		}
		if len(kps) > 0 {
			e.KPName = kps[0].Name
			if kp, err := s.q.GetKP(ctx, dbq.GetKPParams{ID: kps[0].ID, OwnerUserID: owner(userID)}); err == nil {
				e.OriginalText = kp.OriginalText.String
			}
		}
		if ex.SourceMaterialID.Valid {
			if m, err := s.q.GetMaterial(ctx, dbq.GetMaterialParams{ID: uint64(ex.SourceMaterialID.Int64), OwnerUserID: userID}); err == nil {
				e.MaterialID, e.FileName, e.Page = m.ID, m.FileName, int(ex.SourcePage.Int32)
			}
		}
		out.Example = e
	}
	last, err := s.q.LatestGradingOfQType(ctx, dbq.LatestGradingOfQTypeParams{OwnerUserID: userID, BankID: b.BankID, Qtype: qt})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err == nil {
		l := &NormLast{QuestionID: last.QuestionID, Stem: last.Stem, Answer: last.AnswerText.String, GradingID: last.ID, Missing: []string{}}
		if v, ok := parseScore(last.Score); ok {
			l.Score = &v
		}
		if v, ok := parseScore(last.FullScore); ok {
			l.FullScore = &v
		}
		var pts []PointResult
		if last.PointResults != nil {
			_ = json.Unmarshal(last.PointResults, &pts)
		}
		for _, p := range pts {
			if p.Verdict != "hit" {
				l.Missing = append(l.Missing, p.Content)
			}
		}
		out.Last = l
	}
	if id, err := s.q.AnyQuestionOfQType(ctx, dbq.AnyQuestionOfQTypeParams{BankID: b.BankID, OwnerUserID: owner(userID), Qtype: qt}); err == nil {
		out.PracticeID = id
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	return out, nil
}

// NormCheck 是「按结构写一道」的批改结果。
type NormCheck struct {
	GradingID   uint64          `json:"grading_id"`
	Complete    bool            `json:"complete"`
	Elements    []ai.NormResult `json:"elements"`
	Suggestions []string        `json:"suggestions"`
}

// CheckNorm 按题型结构批改（4.13「按结构写一道」）：AI 判断各结构要素是否具备，扣 1 次批改次数（与写结果同一事务）。
// 只看结构，不改掌握分与错题本。同一个幂等键重复提交返回第一次的结果。
func (s *Service) CheckNorm(ctx context.Context, userID, questionID uint64, answer, key string) (NormCheck, error) {
	if len(key) < 8 {
		return NormCheck{}, apperr.New(apperr.BadRequest, "缺少幂等键")
	}
	if prev, err := s.q.GetGradingByKey(ctx, dbq.GetGradingByKeyParams{OwnerUserID: userID, IdempotencyKey: sql.NullString{String: key, Valid: true}}); err == nil {
		return normFromRow(prev), nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return NormCheck{}, err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return NormCheck{}, apperr.New(apperr.BadRequest, "先写点内容再提交")
	}
	qr, err := s.q.GetQuestionFull(ctx, dbq.GetQuestionFullParams{ID: questionID, OwnerUserID: owner(userID)})
	if errors.Is(err, sql.ErrNoRows) {
		return NormCheck{}, apperr.NotFoundErr()
	}
	if err != nil {
		return NormCheck{}, err
	}
	t, ok := normTypes[string(qr.Qtype)]
	if !ok {
		return NormCheck{}, apperr.New(apperr.BadRequest, "这类题型没有答题规范")
	}
	if left, err := s.quota.Remaining(ctx, userID, quota.Grading); err != nil {
		return NormCheck{}, err
	} else if left != nil && *left <= 0 {
		return NormCheck{}, apperr.New(apperr.QuotaExceeded, "今天的批改次数用完了")
	}
	in := ai.NormIn{QType: string(qr.Qtype), Stem: qr.Stem, Answer: answer}
	for _, e := range t.Elements {
		in.Elements = append(in.Elements, ai.NormElement{Name: e.Name, Desc: e.Desc})
	}
	out, meta, err := ai.GradeNorm.Run(ctx, s.ai, userID, in)
	if err != nil {
		return NormCheck{}, err
	}
	res := NormCheck{Elements: out.Elements, Suggestions: out.Suggestions, Complete: true}
	for _, e := range out.Elements {
		res.Complete = res.Complete && e.Present
	}
	now := s.now().UTC()
	err = s.withTx(ctx, func(q *dbq.Queries) error {
		aid, err := q.InsertAttempt(ctx, dbq.InsertAttemptParams{OwnerUserID: userID, QuestionID: qr.ID, AnswerMode: dbq.AttemptsAnswerModeTyped,
			AnswerText: sql.NullString{String: answer, Valid: true}, AnsweredAt: now})
		if err != nil {
			return err
		}
		els, _ := json.Marshal(out.Elements)
		sug, _ := json.Marshal(out.Suggestions)
		gid, err := q.InsertNormGrading(ctx, dbq.InsertNormGradingParams{OwnerUserID: userID, AttemptID: uint64(aid), PointResults: els, Suggestions: sug,
			Model: sql.NullString{String: meta.Model, Valid: meta.Model != ""}, PromptVersion: sql.NullString{String: ai.GradeNorm.Name + "@" + meta.Version, Valid: true},
			IdempotencyKey: sql.NullString{String: key, Valid: true}, FinishedAt: sql.NullTime{Time: now, Valid: true}})
		if err != nil {
			return err
		}
		res.GradingID = uint64(gid)
		return s.quota.Consume(ctx, q, quota.Charge{UserID: userID, Type: quota.Grading, Amount: 1, Ref: quota.Ref{Type: "grading", ID: uint64(gid)},
			Key: quota.KeyFor("grading", uint64(gid))})
	})
	return res, err
}

func normFromRow(g dbq.Grading) NormCheck {
	out := NormCheck{GradingID: g.ID, Complete: true, Suggestions: []string{}}
	if g.PointResults != nil {
		_ = json.Unmarshal(g.PointResults, &out.Elements)
	}
	if g.Suggestions != nil {
		_ = json.Unmarshal(g.Suggestions, &out.Suggestions)
	}
	for _, e := range out.Elements {
		out.Complete = out.Complete && e.Present
	}
	return out
}

// 4.5 拍手写稿（T19）。

const (
	handwritingMax     = 6
	handwritingMaxSize = 10 << 20
	handwritingTTL     = 15 * time.Minute
)

var handwritingExt = map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/heic": "heic", "image/webp": "webp"}

// handwritingPrefix 是用户手写稿照片的对象键前缀；识别时只接受这个前缀下的键（归属检查）。
func handwritingPrefix(userID uint64) string { return "u/" + strconv.FormatUint(userID, 10) + "/hw/" }

// UploadTarget 是一张照片的直传地址。
type UploadTarget struct {
	ObjectKey string
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

// PhotoFile 是一张待上传的照片。
type PhotoFile struct {
	ContentType string
	Size        int64
}

// RequestUploads 申请手写稿照片的直传地址。
func (s *Service) RequestUploads(ctx context.Context, userID uint64, files []PhotoFile) ([]UploadTarget, error) {
	if len(files) == 0 || len(files) > handwritingMax {
		return nil, apperr.New(apperr.BadRequest, "一次最多拍 6 张")
	}
	day := s.now().UTC().Format("20060102")
	out := make([]UploadTarget, 0, len(files))
	for i, f := range files {
		ext, ok := handwritingExt[f.ContentType]
		if !ok {
			return nil, apperr.New(apperr.BadRequest, "只支持 JPG、PNG、HEIC、WebP 照片")
		}
		if f.Size <= 0 || f.Size > handwritingMaxSize {
			return nil, apperr.New(apperr.BadRequest, "单张照片不能超过 10MB")
		}
		key := handwritingPrefix(userID) + day + "/" + strconv.FormatInt(s.now().UnixNano(), 36) + "-" + strconv.Itoa(i) + "." + ext
		p, err := s.oss.PresignPut(ctx, key, f.ContentType, f.Size, handwritingTTL)
		if err != nil {
			return nil, err
		}
		out = append(out, UploadTarget{ObjectKey: key, URL: p.URL, Headers: p.Headers, ExpiresAt: p.ExpiresAt})
	}
	return out, nil
}

// HandwritingPage 是一张照片的识别结果；LowConfidence 按 rune 计。
type HandwritingPage struct {
	ObjectKey     string
	Text          string
	LowConfidence [][2]int
}

// Handwriting 是多张拼接后的识别结果。
type Handwriting struct {
	Text          string
	Pages         []HandwritingPage
	LowConfidence [][2]int
}

// ownPhotos 检查照片键都属于这个用户（别人的键当作不存在）。
func ownPhotos(userID uint64, keys []string) error {
	if len(keys) == 0 || len(keys) > handwritingMax {
		return apperr.New(apperr.BadRequest, "一次最多识别 6 张")
	}
	for _, k := range keys {
		if !strings.HasPrefix(k, handwritingPrefix(userID)) || strings.Contains(k, "..") {
			return apperr.NotFoundErr()
		}
	}
	return nil
}

// Recognize 识别手写稿（4.5）：逐张内容安全检查与手写 OCR，按顺序用换行拼接；不确定的字词换算到全文位置。
func (s *Service) Recognize(ctx context.Context, userID uint64, keys []string) (Handwriting, error) {
	if err := ownPhotos(userID, keys); err != nil {
		return Handwriting{}, err
	}
	var out Handwriting
	offset := 0
	for i, k := range keys {
		info, err := s.oss.Head(ctx, k)
		if err != nil || !info.Exists {
			return Handwriting{}, apperr.NotFoundErr()
		}
		v, err := s.moderation.CheckImage(ctx, k)
		if err != nil {
			return Handwriting{}, err
		}
		if !v.Pass {
			return Handwriting{}, apperr.New(apperr.BadRequest, "第 "+strconv.Itoa(i+1)+" 张照片没有通过内容安全检查，请重拍")
		}
		data, err := s.readObject(ctx, k)
		if err != nil {
			return Handwriting{}, err
		}
		r, err := s.ocr.Recognize(ctx, ocrImage(k, data))
		if err != nil {
			return Handwriting{}, err
		}
		page := HandwritingPage{ObjectKey: k, Text: r.Text}
		if i > 0 {
			out.Text += "\n"
			offset++
		}
		for _, sp := range r.LowConfidence {
			page.LowConfidence = append(page.LowConfidence, [2]int{sp.Start, sp.End})
			out.LowConfidence = append(out.LowConfidence, [2]int{sp.Start + offset, sp.End + offset})
		}
		out.Text += r.Text
		offset += len([]rune(r.Text))
		out.Pages = append(out.Pages, page)
	}
	if strings.TrimSpace(out.Text) == "" {
		return Handwriting{}, apperr.New(apperr.BadRequest, "照片里没有识别到文字。请对准答题纸重拍，保持光线充足、不要反光")
	}
	return out, nil
}

func (s *Service) readObject(ctx context.Context, key string) ([]byte, error) {
	rc, err := s.oss.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, handwritingMaxSize+1))
}

func ocrImage(key string, data []byte) ocr.Image {
	return ocr.Image{ObjectKey: key, Data: data, Handwriting: true}
}
