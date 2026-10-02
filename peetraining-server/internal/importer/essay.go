package importer

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
)

// classify 判断资料类型（PRD 11.12），写进 materials.category / sub_type。只看前 3 页，每页最多 1500 字。
func (s *Service) classify(ctx context.Context, job dbq.GetImportJobRow, m dbq.GetMaterialRow, pages []dbq.MaterialPage) (string, error) {
	in := ai.ClassifyIn{FileName: m.FileName}
	for i, p := range pages {
		if i >= 3 {
			break
		}
		in.Pages = append(in.Pages, ai.KPPage{No: int(p.PageNo), Text: truncate(p.Text, 1500)})
	}
	out, meta, err := ai.Classify.Run(ctx, s.ai, job.OwnerUserID, in)
	if apperr.IsKind(err, apperr.AIFailed) {
		// 判断不出来时按用户在 1.4 选的方式处理。
		return string(job.Mode), nil
	}
	if err != nil {
		return "", err
	}
	s.notePrompt(ctx, job, ai.Classify.Name, meta)
	err = s.q.UpdateMaterialCategory(ctx, dbq.UpdateMaterialCategoryParams{
		Category: dbq.NullMaterialsCategory{MaterialsCategory: dbq.MaterialsCategory(out.Category), Valid: true},
		SubType:  sql.NullString{String: out.SubType, Valid: out.SubType != ""}, ID: m.ID, OwnerUserID: job.OwnerUserID,
	})
	return out.Category, err
}

// 作文资料条目的 payload：AI 输出的结构，页码字段统一为 page（出处）。

// organizeEssay 作文资料整理（T11）：作文真题、评分细则、写作方法、素材、范文都写进待确认。
func (s *Service) organizeEssay(ctx context.Context, job dbq.GetImportJobRow, materialID uint64, pages []dbq.MaterialPage) (int, int, error) {
	in := ai.EssayIn{Subject: job.SubjectName}
	for _, p := range pages {
		in.Pages = append(in.Pages, ai.KPPage{No: int(p.PageNo), Text: p.Text})
	}
	out, meta, err := ai.OrganizeEssay.Run(ctx, s.ai, job.OwnerUserID, in)
	if apperr.IsKind(err, apperr.AIFailed) {
		return 0, 1, nil
	}
	if err != nil {
		return 0, 0, err
	}
	s.notePrompt(ctx, job, ai.OrganizeEssay.Name, meta)
	n := 0
	put := func(typ dbq.ImportItemsItemType, payload any, key string, reasons []string) error {
		n++
		return s.putEssayItem(ctx, job, materialID, typ, payload, key, reasons)
	}
	for _, t := range out.Topics {
		if err := put(dbq.ImportItemsItemTypeEssayTopic, t, "topic:"+Normalize(t.Title), nil); err != nil {
			return n, 0, err
		}
	}
	if out.Rubric != nil {
		if err := put(dbq.ImportItemsItemTypeEssayRubric, out.Rubric, "rubric:"+strconv.FormatUint(materialID, 10), nil); err != nil {
			return n, 0, err
		}
	}
	for _, m := range out.Methods {
		if err := put(dbq.ImportItemsItemTypeWritingMethod, m, "method:"+Normalize(m.Title), nil); err != nil {
			return n, 0, err
		}
	}
	for _, m := range out.Materials {
		if err := put(dbq.ImportItemsItemTypeEssayMaterial, m, "material:"+m.Theme+":"+Normalize(truncate(m.Content, 60)), nil); err != nil {
			return n, 0, err
		}
	}
	for _, m := range out.Models {
		if err := put(dbq.ImportItemsItemTypeModelEssay, m, "model:"+Normalize(m.Title), nil); err != nil {
			return n, 0, err
		}
	}
	return n, 0, nil
}

func (s *Service) putEssayItem(ctx context.Context, job dbq.GetImportJobRow, materialID uint64, typ dbq.ImportItemsItemType, payload any, key string, reasons []string) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var page struct{ Page int }
	_ = json.Unmarshal(b, &page)
	return s.q.UpsertImportItem(ctx, dbq.UpsertImportItemParams{
		OwnerUserID: job.OwnerUserID, JobID: job.ID, MaterialID: sql.NullInt64{Int64: int64(materialID), Valid: true},
		ItemType: typ, Seq: uint32(page.Page) * 1000, Payload: b, Confidence: sql.NullString{String: "0.900", Valid: true},
		NeedsReview: needsReview(reasons), ReviewReasons: reasonsJSON(reasons), DedupeKey: sql.NullString{String: hashOf(string(typ), key), Valid: true},
	})
}

// detectEssaySubject 按这门课已解析资料的类型更新作文课判断（PRD 11.12）：作文类过半即作文课；用户改过的不动。
func (s *Service) detectEssaySubject(ctx context.Context, userID uint64, subjectID sql.NullInt64) error {
	if !subjectID.Valid {
		return nil
	}
	c, err := s.q.CountSubjectMaterialCategories(ctx, dbq.CountSubjectMaterialCategoriesParams{SubjectID: subjectID, OwnerUserID: userID})
	if err != nil || c.Total == 0 {
		return err
	}
	return s.q.SetSubjectEssayAuto(ctx, dbq.SetSubjectEssayAutoParams{IsEssay: c.Essay*2 > c.Total, ID: uint64(subjectID.Int64), OwnerUserID: userID})
}

// essayOrder 是确认时作文条目的处理顺序：先建作文题，范文才能按题目归类。
var essayOrder = map[dbq.ImportItemsItemType]int{
	dbq.ImportItemsItemTypeEssayTopic: 1, dbq.ImportItemsItemTypeEssayRubric: 2, dbq.ImportItemsItemTypeWritingMethod: 3,
	dbq.ImportItemsItemTypeEssayMaterial: 4, dbq.ImportItemsItemTypeModelEssay: 5,
}

// confirmEssayItem 把一条作文资料入库，返回生成的记录 ID。
func (s *Service) confirmEssayItem(ctx context.Context, q *dbq.Queries, job dbq.GetImportJobRow, it dbq.ImportItem) (uint64, error) {
	owner := sql.NullInt64{Int64: int64(job.OwnerUserID), Valid: true}
	edited := it.Status == dbq.ImportItemsStatusEdited
	page := func(p int) sql.NullInt32 { return sql.NullInt32{Int32: int32(p), Valid: p > 0} }
	switch it.ItemType {
	case dbq.ImportItemsItemTypeEssayTopic:
		var t ai.EssayTopic
		if err := json.Unmarshal(it.Payload, &t); err != nil {
			return 0, err
		}
		p := dbq.InsertQuestionParams{OwnerUserID: owner, BankID: job.BankID, Qtype: dbq.QuestionsQtypeEssay, Stem: t.Title,
			Source: dbq.QuestionsSourceExercise, SourceMaterialID: it.MaterialID, SourcePage: page(t.Page), ContentHash: ContentHash(t.Title),
			Difficulty: dbq.NullQuestionsDifficulty{QuestionsDifficulty: dbq.QuestionsDifficultyMedium, Valid: true}}
		if t.Year != nil {
			p.Source, p.ExamYear = dbq.QuestionsSourceExam, sql.NullInt16{Int16: int16(*t.Year), Valid: true}
		}
		id, err := q.InsertQuestion(ctx, p)
		if err != nil {
			return 0, err
		}
		if t.RequiredWords != nil {
			if err := q.SetQuestionRequiredWords(ctx, dbq.SetQuestionRequiredWordsParams{RequiredWords: sql.NullInt16{Int16: int16(*t.RequiredWords), Valid: true}, ID: uint64(id), OwnerUserID: owner}); err != nil {
				return 0, err
			}
		}
		return uint64(id), nil
	case dbq.ImportItemsItemTypeEssayRubric:
		var r ai.EssayRubric
		if err := json.Unmarshal(it.Payload, &r); err != nil {
			return 0, err
		}
		dims, _ := json.Marshal(r.Dimensions)
		origin := dbq.EssayRubricsOriginAiExtracted
		if edited {
			origin = dbq.EssayRubricsOriginUserConfirmed
		}
		// 新标准生效，之前的停用（5.9：改了标准后新写的作文按新标准批改）。
		if err := q.DeactivateEssayRubrics(ctx, dbq.DeactivateEssayRubricsParams{OwnerUserID: owner, SubjectID: job.SubjectID}); err != nil {
			return 0, err
		}
		id, err := q.InsertEssayRubric(ctx, dbq.InsertEssayRubricParams{OwnerUserID: owner, SubjectID: job.SubjectID, Name: truncate(r.Name, 64),
			FullScore: decimal(r.FullScore), Dimensions: dims, SourceMaterialID: it.MaterialID, SourcePage: nullUint(r.Page), Origin: origin})
		return uint64(id), err
	case dbq.ImportItemsItemTypeWritingMethod:
		var m ai.WritingMethod
		if err := json.Unmarshal(it.Payload, &m); err != nil {
			return 0, err
		}
		id, err := q.InsertWritingMethod(ctx, dbq.InsertWritingMethodParams{OwnerUserID: owner, BankID: job.BankID, Title: truncate(m.Title, 128),
			Content: m.Content, Dimension: sql.NullString{String: truncate(m.Dimension, 32), Valid: m.Dimension != ""},
			SourceMaterialID: it.MaterialID, SourcePage: nullUint(m.Page), Origin: methodOrigin(edited)})
		return uint64(id), err
	case dbq.ImportItemsItemTypeEssayMaterial:
		var m ai.EssayMaterial
		if err := json.Unmarshal(it.Payload, &m); err != nil {
			return 0, err
		}
		origin := dbq.EssayMaterialsOriginAiExtracted
		switch {
		case m.AISupplement:
			origin = dbq.EssayMaterialsOriginAiGenerated
		case edited:
			origin = dbq.EssayMaterialsOriginUserConfirmed
		}
		id, err := q.InsertEssayMaterial(ctx, dbq.InsertEssayMaterialParams{OwnerUserID: owner, BankID: job.BankID, Theme: truncate(m.Theme, 64),
			Content: m.Content, SourceMaterialID: it.MaterialID, SourcePage: nullUint(m.Page), Origin: origin})
		return uint64(id), err
	case dbq.ImportItemsItemTypeModelEssay:
		var m ai.ModelEssay
		if err := json.Unmarshal(it.Payload, &m); err != nil {
			return 0, err
		}
		st, _ := json.Marshal(m.Structure)
		topic := sql.NullInt64{}
		if m.Topic != "" {
			topics, err := q.ListBankEssayTopics(ctx, dbq.ListBankEssayTopicsParams{BankID: job.BankID, OwnerUserID: owner})
			if err != nil {
				return 0, err
			}
			for _, t := range topics {
				if ai.Overlap(m.Topic, t.Stem) >= 0.8 || strings.Contains(ai.Compact(t.Stem), ai.Compact(m.Topic)) {
					topic = sql.NullInt64{Int64: int64(t.ID), Valid: true}
					break
				}
			}
		}
		id, err := q.InsertModelEssay(ctx, dbq.InsertModelEssayParams{OwnerUserID: owner, BankID: job.BankID, TopicQuestionID: topic,
			Title: truncate(m.Title, 128), Content: m.Content, Structure: st, SourceMaterialID: it.MaterialID, SourcePage: nullUint(m.Page)})
		return uint64(id), err
	}
	return 0, nil
}

func methodOrigin(edited bool) dbq.WritingMethodsOrigin {
	if edited {
		return dbq.WritingMethodsOriginUserConfirmed
	}
	return dbq.WritingMethodsOriginAiExtracted
}

func nullUint(p int) sql.NullInt32 { return sql.NullInt32{Int32: int32(p), Valid: p > 0} }
