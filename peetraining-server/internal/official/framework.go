package official

import (
	"context"
	"strings"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
)

// T54 知识框架直接从项目已关联的资料生成，不用再传一次：考纲用全文（考点条目就是框架），
// 教材、讲义只用章节标题（相当于参考书目录，全文太长）；经验总结不是知识内容，不能生成。
// 生成结果是一份「参考书目录」解析记录，走原来的知识点候选与核对（parseCatalog）。

// SectionLoader 读一个版本当前发布修订的章节（由资料库服务提供）。
type SectionLoader func(ctx context.Context, book, edition, revision uint64) ([]rules.TextbookSection, error)

// FrameworkText 是拿去生成知识框架的文字。
func FrameworkText(kind string, sections []rules.TextbookSection) string {
	var b strings.Builder
	for _, s := range sections {
		h := strings.TrimSpace(s.Heading)
		switch kind {
		case rules.MaterialSyllabus:
			if h != "" {
				b.WriteString(h + "\n")
			}
			if t := strings.TrimSpace(s.Text); t != "" {
				b.WriteString(t + "\n")
			}
		default:
			if h != "" {
				b.WriteString(h + "\n")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// CreateFrameworkImport 用项目关联的某个资料版本生成知识框架，返回新建的解析记录。
func (s *Service) CreateFrameworkImport(ctx context.Context, adminID, projectID, editionID uint64, load SectionLoader) (ImportFile, error) {
	if _, err := s.projectExists(ctx, projectID); err != nil {
		return ImportFile{}, err
	}
	links, err := s.q.ListProjectTextbooks(ctx, projectID)
	if err != nil {
		return ImportFile{}, err
	}
	var e *dbq.ListProjectTextbooksRow
	for i := range links {
		if links[i].ID == editionID {
			e = &links[i]
		}
	}
	if e == nil {
		return ImportFile{}, apperr.NotFoundErr()
	}
	if e.Kind == rules.MaterialExperience {
		return ImportFile{}, apperr.New(apperr.BadRequest, "经验总结不能生成知识框架，请选考纲、教材或讲义")
	}
	if e.Status != "published" || !e.CurrentRevisionID.Valid {
		return ImportFile{}, apperr.New(apperr.BadRequest, "这份资料还没有发布原文，先在资料库发布")
	}
	sections, err := load(ctx, e.BookID, e.ID, uint64(e.CurrentRevisionID.Int64))
	if err != nil {
		return ImportFile{}, err
	}
	text := FrameworkText(e.Kind, sections)
	if len([]rune(text)) < 10 {
		return ImportFile{}, apperr.New(apperr.BadRequest, "这份资料没有章节标题，生成不了知识框架；可以上传参考书目录")
	}
	name := e.Title + " " + e.Edition + "（知识框架）"
	return s.CreateImportFile(ctx, adminID, projectID, ImportFileInput{Kind: string(dbq.OfficialImportFilesKindCatalog), Format: string(dbq.OfficialImportFilesFormatText), FileName: name, Text: text})
}
