package textbook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/flags"
	"peetraining-server/internal/rules"

	"golang.org/x/sync/errgroup"
)

func (s *Service) ConfigureEvidence(engine *ai.Engine, f *flags.Service) {
	s.engine = engine
	s.flags = f
}

// MatchEditions 只匹配当前可用版次；无默认的多版本、同名异作者保持未确定。
// 套书（上下卷等）按套书名整套匹配，每册各取一个版次。
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
		var aliases []string
		_ = json.Unmarshal(book.Aliases, &aliases)
		titles := append([]string{book.Title}, aliases...)
		if book.Series != "" {
			titles = append(titles, book.Series)
		}
		for _, v := range nameVariants(titles, book.Author, e.Edition) {
			explicitNames[v] = true
		}
	}
	for _, id := range explicit {
		for _, e := range rows {
			if e.ID == id {
				add(e)
			}
		}
	}
	// 按书名匹配（考生填的参考书）只认教材、讲义；考纲、经验总结只能由官方项目明确关联（T55）。
	named := slices.DeleteFunc(slices.Clone(rows), func(e dbq.ListPublishedTextbookEditionsRow) bool { return !rules.IsKnowledgeMaterial(e.Kind) })
	for _, name := range names {
		input := rules.NormalizeTextbookName(name)
		if explicitNames[input] {
			continue
		}
		for _, e := range matchByName(named, input, subject) {
			add(e)
		}
	}
	return out, nil
}

// nameVariants 是一本书可被识别的写法：书名或别名，可带作者（前后均可）和版次，均已规范化。
func nameVariants(titles []string, author, edition string) []string {
	out := make([]string, 0, len(titles)*7)
	for _, t := range titles {
		for _, v := range []string{t, t + author, author + t, author + t + edition, t + edition, t + author + edition, t + edition + author} {
			out = append(out, rules.NormalizeTextbookName(v))
		}
	}
	return out
}

func hasVariant(input string, titles []string, author, edition string) bool {
	for _, v := range nameVariants(titles, author, edition) {
		if v == input {
			return true
		}
	}
	return false
}

// matchByName 把书名解析为唯一的一本书或一整套书。命中多个选项时只用严格相同学科消歧，仍不唯一则不匹配。
func matchByName(rows []dbq.ListPublishedTextbookEditionsRow, input, subject string) []dbq.ListPublishedTextbookEditionsRow {
	type option struct {
		rows   []dbq.ListPublishedTextbookEditionsRow
		series bool
	}
	options := map[string]*option{}
	order := []string{}
	put := func(key string, series bool, e dbq.ListPublishedTextbookEditionsRow) {
		o := options[key]
		if o == nil {
			o = &option{series: series}
			options[key] = o
			order = append(order, key)
		}
		o.rows = append(o.rows, e)
	}
	seriesOf := map[uint64]string{}
	for _, e := range rows {
		titles := []string{e.Title}
		var more []string
		_ = json.Unmarshal(e.Aliases, &more)
		titles = append(titles, more...)
		if hasVariant(input, titles, e.Author, e.Edition) {
			put(fmt.Sprintf("book:%d", e.BookID), false, e)
		}
		if e.Series != "" {
			// 同一套书要求套书名和作者都相同，避免不同作者的同名套书被并成一套。
			key := "series:" + rules.NormalizeTextbookName(e.Series) + "\x00" + rules.NormalizeTextbookName(e.Author)
			seriesOf[e.BookID] = key
			if hasVariant(input, []string{e.Series}, e.Author, e.Edition) {
				put(key, true, e)
			}
		}
	}
	// 某册书名恰好等于套书名时，整套优先，不再单独算这一册。
	keys := []string{}
	for _, k := range order {
		o := options[k]
		if !o.series {
			if sk := seriesOf[o.rows[0].BookID]; sk != "" && options[sk] != nil {
				continue
			}
		}
		keys = append(keys, k)
	}
	if len(keys) > 1 {
		filtered := []string{}
		for _, k := range keys {
			for _, e := range options[k].rows {
				if rules.NormalizeTextbookName(e.Discipline) == rules.NormalizeTextbookName(subject) {
					filtered = append(filtered, k)
					break
				}
			}
		}
		keys = filtered
	}
	if len(keys) != 1 {
		return nil
	}
	byBook := map[uint64][]dbq.ListPublishedTextbookEditionsRow{}
	books := []uint64{}
	for _, e := range options[keys[0]].rows {
		if byBook[e.BookID] == nil {
			books = append(books, e.BookID)
		}
		byBook[e.BookID] = append(byBook[e.BookID], e)
	}
	// 套书按书名排序，上卷在前、下卷在后。
	sort.SliceStable(books, func(i, j int) bool { return byBook[books[i]][0].Title < byBook[books[j]][0].Title })
	out := []dbq.ListPublishedTextbookEditionsRow{}
	for _, b := range books {
		candidates := byBook[b]
		if len(candidates) == 1 {
			out = append(out, candidates[0])
			continue
		}
		for _, e := range candidates {
			if e.DefaultEditionID.Valid && uint64(e.DefaultEditionID.Int64) == e.ID {
				out = append(out, e)
				break
			}
		}
	}
	return out
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
	if user != 0 {
		// 考生的答案只用教材、讲义（Q20：考纲、经验总结先不给考生）。
		editions = slices.DeleteFunc(editions, func(e dbq.ListPublishedTextbookEditionsRow) bool { return !rules.IsKnowledgeMaterial(e.Kind) })
	}
	// T53 记下没用上教材的原因：关联了哪些版本、哪本为什么不能检索。
	details, knowledgeDown := s.unavailableEditions(ctx, in.EditionIDs, editions)
	if len(in.EditionIDs) == 0 && len(editions) == 0 {
		in.Basis = &ai.AnswerBasis{Reason: ai.BasisNoTextbook}
		return in, nil
	}
	// T55 按类型分名额：教材讲义是知识依据占大头，考纲、经验总结各取几段，免得被教材挤掉。
	caps := map[string]int{rules.MaterialSyllabus: 3, rules.MaterialExperience: 3}
	group := func(kind string) string {
		if rules.IsKnowledgeMaterial(kind) {
			return rules.MaterialTextbook
		}
		return kind
	}
	caps[rules.MaterialTextbook] = 16 - caps[rules.MaterialSyllabus] - caps[rules.MaterialExperience]
	// 关联的每本书都检索（并发），再按相似度从所有书的段落里取名额内的前几段。
	// 以前按关联顺序一本本取、名额满了后面的书就不查，关联多本书时相关的那本常常轮不到。
	results := s.searchEditions(ctx, editions, query)
	type candidate struct {
		fragment ai.EvidenceFragment
		score    float64
		order    int
	}
	candidates := map[string][]candidate{}
	knowledgeLinked, knowledgeSearched := knowledgeDown, 0
	for i, e := range editions {
		g := group(e.Kind)
		if g == rules.MaterialTextbook {
			knowledgeLinked++
		}
		result, err := results[i].result, results[i].err
		if err != nil {
			details = append(details, e.Title+"："+errText(err))
			continue
		}
		if g == rules.MaterialTextbook {
			knowledgeSearched++
		}
		score := bookOrderScores(result.Hits)
		for j, h := range result.Hits {
			candidates[g] = append(candidates[g], candidate{order: len(candidates[g]), score: score[j], fragment: ai.EvidenceFragment{ID: fmt.Sprintf("textbook:%d:%d", result.GenerationID, h.Ordinal), Kind: "official_textbook", Material: e.Kind, Title: e.Title, Author: e.Author, Edition: e.Edition, Text: h.Text, BookID: e.BookID, EditionID: e.ID, RevisionID: uint64(e.CurrentRevisionID.Int64), GenerationID: result.GenerationID, Ordinal: h.Ordinal, SectionNo: h.SectionNo, StartRune: h.StartRune, EndRune: h.EndRune, TextHash: h.TextHash, Heading: h.Heading, SourcePage: h.SourcePage, PrintedPage: h.PrintedPage}})
		}
	}
	picked := map[string][]ai.EvidenceFragment{}
	for g, list := range candidates {
		sort.SliceStable(list, func(a, b int) bool {
			if list[a].score != list[b].score {
				return list[a].score > list[b].score
			}
			return list[a].order < list[b].order
		})
		for _, c := range list {
			if len(picked[g]) >= caps[g] {
				break
			}
			picked[g] = append(picked[g], c.fragment)
		}
	}
	evidence := append(append(picked[rules.MaterialTextbook], picked[rules.MaterialSyllabus]...), picked[rules.MaterialExperience]...)
	// 没有教材讲义可用时先记原因（考纲、经验总结照样往下用）。
	switch {
	case knowledgeLinked == 0:
		in.Basis = &ai.AnswerBasis{Reason: ai.BasisNoTextbook, Details: details}
	case knowledgeSearched == 0:
		in.Basis = &ai.AnswerBasis{Reason: ai.BasisUnavailable, Details: details}
	case len(picked[rules.MaterialTextbook]) == 0:
		in.Basis = &ai.AnswerBasis{Reason: ai.BasisNoHits, Details: details}
	}
	if len(evidence) == 0 {
		return in, nil
	}
	selection, _, err := ai.SelectAnswerEvidence.Run(ctx, s.engine, user, ai.SelectEvidenceIn{Subject: in.Subject, QType: in.QType, Stem: in.Stem, Evidence: evidence})
	if err != nil {
		return in, err
	}
	if in.Basis == nil && !slices.ContainsFunc(selection.SourceIDs, func(id string) bool {
		return slices.ContainsFunc(evidence, func(f ai.EvidenceFragment) bool { return f.ID == id && f.IsKnowledge() })
	}) {
		in.Basis = &ai.AnswerBasis{Reason: ai.BasisNotRelevant, Details: details}
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

type editionSearch struct {
	result SearchResult
	err    error
}

// searchEditions 并发检索每本书（最多同时 4 本），检索问题的向量只算一次；结果与 editions 一一对应。
func (s *Service) searchEditions(ctx context.Context, editions []dbq.ListPublishedTextbookEditionsRow, query string) []editionSearch {
	out := make([]editionSearch, len(editions))
	var embedding []float64
	if len(editions) > 1 && s.indexes.Embedding != nil {
		// 算不出来时不中断：每本书检索时再各自算，失败原因记到那本书上。
		if v, err := s.indexes.Embedding.Embed(ctx, []string{query}); err == nil && len(v) == 1 {
			embedding = v[0]
		}
	}
	var g errgroup.Group
	g.SetLimit(4)
	for i, e := range editions {
		g.Go(func() error {
			r, err := s.searchRevision(ctx, e.BookID, e.ID, uint64(e.CurrentRevisionID.Int64), uint64(e.CurrentGenerationID.Int64), query, embedding)
			out[i] = editionSearch{r, err}
			return nil
		})
	}
	_ = g.Wait()
	return out
}

// bookOrderScores 是一本书各段落参与跨书排序的分数：向量相似度，但不超过它前面的段落，
// 保持书内（全文 + 向量融合后）的顺序；只有全文命中的段落沿用前一段的分数。
func bookOrderScores(hits []SearchHit) []float64 {
	out := make([]float64, len(hits))
	floor := 0.0
	for _, h := range hits {
		floor = max(floor, h.VectorScore)
	}
	for i, h := range hits {
		if h.VectorRank > 0 {
			floor = min(floor, h.VectorScore)
		}
		out[i] = floor
	}
	return out
}

// unavailableEditions 是明确关联、但不在可检索列表里的版本及原因（未发布、授权不可用、还没有索引）。
// 第二个返回值是其中教材、讲义的本数（用来区分「没关联教材」与「关联的教材不能检索」）。
func (s *Service) unavailableEditions(ctx context.Context, explicit []uint64, usable []dbq.ListPublishedTextbookEditionsRow) ([]string, int) {
	var out []string
	knowledge := 0
	for _, id := range explicit {
		if slices.ContainsFunc(usable, func(e dbq.ListPublishedTextbookEditionsRow) bool { return e.ID == id }) {
			continue
		}
		e, err := dbq.New(s.db).GetTextbookEditionByID(ctx, id)
		if err != nil {
			continue
		}
		title := e.Edition
		if b, err := dbq.New(s.db).GetTextbook(ctx, e.BookID); err == nil {
			title = b.Title + " " + e.Edition
			if rules.IsKnowledgeMaterial(b.Kind) {
				knowledge++
			}
		}
		switch {
		case e.Status != "published":
			out = append(out, title+"：还没发布")
		case !validLicense(e, s.now().UTC()):
			out = append(out, title+"：授权不可用")
		default:
			out = append(out, title+"：还没有可检索的索引")
		}
	}
	return out, knowledge
}

// errText 是检索失败时给编辑看的原因：业务错误原样给，其余统一说检索出错。
func errText(err error) string {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Message
	}
	return "检索出错"
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

// ValidateAnswerSourcesBatch 一次核对多份答案依据，结果与逐份调用 ValidateAnswerSources 相同（ok[i] 为真表示第 i 份仍有效）。
// 添加官方题库时几百道题各带依据，逐份核对要上千次查库；这里同一教材版本只锁一次、同一索引只查一次、片段按索引整批取。
// 查库出错时那份算失效（与逐份核对一致），但请求已取消时返回错误，免得把依据全当失效丢掉。
func (s *Service) ValidateAnswerSourcesBatch(ctx context.Context, q *dbq.Queries, batch [][]ai.AnswerSource) ([]bool, error) {
	type editionKey struct{ id, book uint64 }
	editions := map[editionKey]bool{}
	generations := map[uint64]*dbq.TextbookIndexGeneration{}
	ordinals := map[uint64][]uint32{}
	for _, sources := range batch {
		first := map[uint64]uint64{}
		for _, src := range sources {
			if src.Kind != "official_textbook" {
				continue
			}
			if _, ok := first[src.EditionID]; !ok {
				first[src.EditionID] = src.BookID
				editions[editionKey{src.EditionID, src.BookID}] = false
			}
			generations[src.GenerationID] = nil
			ordinals[src.GenerationID] = append(ordinals[src.GenerationID], src.Ordinal)
		}
	}
	keys := make([]editionKey, 0, len(editions))
	for k := range editions {
		keys = append(keys, k)
	}
	// 按 id 顺序加锁，和逐份核对一样，避免并发时互相等待成死锁。
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].id < keys[j].id || (keys[i].id == keys[j].id && keys[i].book < keys[j].book)
	})
	now := s.now().UTC()
	for _, k := range keys {
		e, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: k.id, BookID: k.book})
		editions[k] = err == nil && e.Status == "published" && validLicense(e, now)
	}
	chunks := map[uint64]map[uint32]dbq.TextbookChunk{}
	for id := range generations {
		if g, err := q.GetTextbookIndex(ctx, id); err == nil {
			generations[id] = &g
		}
		rows, err := q.ListTextbookChunksByOrdinals(ctx, dbq.ListTextbookChunksByOrdinalsParams{GenerationID: id, Ordinals: ordinals[id]})
		if err != nil {
			continue
		}
		chunks[id] = map[uint32]dbq.TextbookChunk{}
		for _, c := range rows {
			chunks[id][c.Ordinal] = c
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]bool, len(batch))
	for i, sources := range batch {
		ok := true
		first := map[uint64]uint64{}
		for _, src := range sources {
			if src.Kind != "official_textbook" {
				continue
			}
			if _, seen := first[src.EditionID]; !seen {
				first[src.EditionID] = src.BookID
				if !editions[editionKey{src.EditionID, src.BookID}] {
					ok = false
					break
				}
			}
			g := generations[src.GenerationID]
			if g == nil || g.Status != "ready" || g.BookID != src.BookID || g.EditionID != src.EditionID || g.RevisionID != src.RevisionID {
				ok = false
				break
			}
			chunk, found := chunks[src.GenerationID][src.Ordinal]
			if !found || chunk.TextHash != src.TextHash || !strings.Contains(chunk.Body, src.Quote) || chunk.SectionNo != src.SectionNo || chunk.StartRune != src.StartRune || chunk.EndRune != src.EndRune {
				ok = false
				break
			}
		}
		out[i] = ok
	}
	return out, nil
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
	// 考纲、经验总结只在后台用，考生看到的出处里去掉（Q20，T55）。
	out.Sources = slices.DeleteFunc(out.Sources, func(src ai.AnswerSource) bool {
		return src.Kind == "official_textbook" && src.Material != "" && !rules.IsKnowledgeMaterial(src.Material)
	})
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
