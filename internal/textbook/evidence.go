package textbook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/rules"
)

func (s *Service) ConfigureEvidence(engine *ai.Engine, f *flags.Service) {
	s.engine = engine
	s.flags = f
}

// MatchEditions 只匹配当前可用版次；无默认的多版本、同名异作者保持未确定。
func (s *Service) MatchEditions(ctx context.Context, names []string, subject string, explicit []uint64) ([]dbq.ListPublishedTextbookEditionsRow, error) {
	rows, err := dbq.New(s.db).ListPublishedTextbookEditions(ctx, sql.NullTime{Time: s.now().UTC(), Valid: true})
	if err != nil {
		return nil, err
	}
	out := []dbq.ListPublishedTextbookEditionsRow{}
	seen := map[uint64]bool{}
	add := func(e dbq.ListPublishedTextbookEditionsRow) {
		if !seen[e.ID] && e.CurrentRevisionID.Valid && e.CurrentGenerationID.Valid {
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	explicitNames := map[string]bool{}
	for _, id := range explicit {
		e, err := dbq.New(s.db).GetTextbookEditionByID(ctx, id)
		if err != nil {
			continue
		}
		book, err := dbq.New(s.db).GetTextbook(ctx, e.BookID)
		if err != nil {
			continue
		}
		names := []string{book.Title}
		var aliases []string
		_ = json.Unmarshal(book.Aliases, &aliases)
		names = append(names, aliases...)
		for _, name := range names {
			for _, v := range []string{name, name + book.Author, book.Author + name, book.Author + name + e.Edition, name + e.Edition, name + book.Author + e.Edition, name + e.Edition + book.Author} {
				explicitNames[rules.NormalizeTextbookName(v)] = true
			}
		}
	}
	for _, id := range explicit {
		for _, e := range rows {
			if e.ID == id {
				add(e)
			}
		}
	}
	for _, name := range names {
		input := rules.NormalizeTextbookName(name)
		if explicitNames[input] {
			continue
		}
		candidates := []dbq.ListPublishedTextbookEditionsRow{}
		for _, e := range rows {
			aliases := []string{e.Title}
			var more []string
			_ = json.Unmarshal(e.Aliases, &more)
			aliases = append(aliases, more...)
			match := false
			for _, alias := range aliases {
				for _, v := range []string{alias, alias + e.Author, e.Author + alias, e.Author + alias + e.Edition, alias + e.Edition, alias + e.Author + e.Edition, alias + e.Edition + e.Author} {
					if input == rules.NormalizeTextbookName(v) {
						match = true
					}
				}
			}
			if match {
				candidates = append(candidates, e)
			}
		}
		// 精确同名有多书时只用严格相同学科消歧，不把相近学科当确定匹配。
		books := map[uint64]bool{}
		for _, e := range candidates {
			books[e.BookID] = true
		}
		if len(books) > 1 {
			filtered := candidates[:0]
			for _, e := range candidates {
				if rules.NormalizeTextbookName(e.Discipline) == rules.NormalizeTextbookName(subject) {
					filtered = append(filtered, e)
				}
			}
			candidates = filtered
			books = map[uint64]bool{}
			for _, e := range candidates {
				books[e.BookID] = true
			}
		}
		if len(books) != 1 {
			continue
		}
		if len(candidates) == 1 {
			add(candidates[0])
			continue
		}
		for _, e := range candidates {
			if e.DefaultEditionID.Valid && uint64(e.DefaultEditionID.Int64) == e.ID {
				add(e)
				break
			}
		}
	}
	return out, nil
}
func (s *Service) PrepareAnswer(ctx context.Context, user uint64, in ai.GenerateAnswerIn) (ai.GenerateAnswerIn, error) {
	if s.flags == nil || s.engine == nil {
		return in, nil
	}
	enabled, err := s.flags.Enabled(ctx, flags.TextbookRetrieval, user)
	if err != nil || (!enabled && (user != 0 || len(in.EditionIDs) == 0)) {
		return in, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	query := in.Stem
	if len([]rune(query)) > 1800 {
		out, _, err := ai.ExtractEvidenceQuery.Run(ctx, s.engine, user, ai.EvidenceQueryIn{Subject: in.Subject, QType: in.QType, Stem: in.Stem})
		if err != nil {
			return in, err
		}
		query = out.Query
	}
	editions, err := s.MatchEditions(ctx, in.Textbooks, in.Subject, in.EditionIDs)
	if err != nil {
		return in, err
	}
	evidence := []ai.EvidenceFragment{}
	for _, e := range editions {
		result, err := s.SearchRevision(ctx, e.BookID, e.ID, uint64(e.CurrentRevisionID.Int64), uint64(e.CurrentGenerationID.Int64), query)
		if err != nil {
			continue
		}
		for _, h := range result.Hits {
			evidence = append(evidence, ai.EvidenceFragment{ID: fmt.Sprintf("textbook:%d:%d", result.GenerationID, h.Ordinal), Kind: "official_textbook", Title: e.Title, Author: e.Author, Edition: e.Edition, Text: h.Text, BookID: e.BookID, EditionID: e.ID, RevisionID: uint64(e.CurrentRevisionID.Int64), GenerationID: result.GenerationID, Ordinal: h.Ordinal, SectionNo: h.SectionNo, StartRune: h.StartRune, EndRune: h.EndRune, TextHash: h.TextHash, Heading: h.Heading, SourcePage: h.SourcePage, PrintedPage: h.PrintedPage})
		}
		if len(evidence) >= 16 {
			evidence = evidence[:min(16, len(evidence))]
			break
		}
	}
	if len(evidence) == 0 {
		return in, nil
	}
	selection, _, err := ai.SelectAnswerEvidence.Run(ctx, s.engine, user, ai.SelectEvidenceIn{Subject: in.Subject, QType: in.QType, Stem: in.Stem, Evidence: evidence})
	if err != nil {
		return in, err
	}
	for _, id := range selection.SourceIDs {
		for _, f := range evidence {
			if f.ID == id {
				in.Evidence = append(in.Evidence, f)
				break
			}
		}
	}
	in.Coverage = selection.Coverage
	return in, nil
}

// ValidateAnswerSources 在写入事务中锁定所有版次，阻止下架/授权撤销与最终保存交错。
// 普通新修订发布不撤销锁定旧修订，历史答案仍可按已选合法代次完成。
func (s *Service) ValidateAnswerSources(ctx context.Context, q *dbq.Queries, sources []ai.AnswerSource) error {
	ids := []uint64{}
	seen := map[uint64]bool{}
	for _, src := range sources {
		if src.Kind == "official_textbook" && !seen[src.EditionID] {
			seen[src.EditionID] = true
			ids = append(ids, src.EditionID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		var src ai.AnswerSource
		for _, v := range sources {
			if v.EditionID == id {
				src = v
				break
			}
		}
		e, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: id, BookID: src.BookID})
		if err != nil {
			return sourceChanged()
		}
		if e.Status != "published" || !validLicense(e, s.now().UTC()) {
			return sourceChanged()
		}
	}
	for _, src := range sources {
		if src.Kind != "official_textbook" {
			continue
		}
		g, err := q.GetTextbookIndex(ctx, src.GenerationID)
		if err != nil {
			return sourceChanged()
		}
		if g.Status != "ready" || g.BookID != src.BookID || g.EditionID != src.EditionID || g.RevisionID != src.RevisionID {
			return sourceChanged()
		}
		chunk, err := q.GetTextbookChunk(ctx, dbq.GetTextbookChunkParams{GenerationID: g.ID, Ordinal: src.Ordinal})
		if err != nil || chunk.TextHash != src.TextHash || !strings.Contains(chunk.Body, src.Quote) || chunk.SectionNo != src.SectionNo || chunk.StartRune != src.StartRune || chunk.EndRune != src.EndRune {
			return sourceChanged()
		}
	}
	return nil
}
func sourceChanged() error {
	return apperr.New(apperr.AIFailed, "依据已变化，请重新生成，未扣除次数")
}

// VisibleSnapshot 只返回经过显示授权检查的摘录。普通运营下架不隐藏已有历史摘录。
func VisibleSnapshot(ctx context.Context, q *dbq.Queries, snapshot *ai.AnswerSnapshot) (*ai.AnswerSnapshot, error) {
	if snapshot == nil {
		return nil, nil
	}
	data, _ := json.Marshal(snapshot)
	var out ai.AnswerSnapshot
	_ = json.Unmarshal(data, &out)
	for i, src := range out.Sources {
		if src.Kind != "official_textbook" {
			continue
		}
		e, err := q.GetTextbookEdition(ctx, dbq.GetTextbookEditionParams{ID: src.EditionID, BookID: src.BookID})
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err != nil || !e.ExcerptsEnabled {
			out.Sources[i].Quote = ""
			out.Sources[i].Claim = ""
			out.Sources[i].Text = ""
			out.Sources[i].Unavailable = true
		}
	}
	return &out, nil
}
