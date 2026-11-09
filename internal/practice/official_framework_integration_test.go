package practice_test

import (
	"context"
	"testing"
	"time"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/official"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/textbook"
)

// TestOfficialFrameworkFromMaterials 是 T54 的验收：用项目已关联的考纲、教材生成知识框架，结果进核对区；
// 经验总结不能生成；没关联的资料、别的项目 404。
func TestOfficialFrameworkFromMaterials(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	s := official.New(official.Deps{DB: f.db, AI: ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true}), Now: func() time.Time { return f.clock }})
	books := textbook.New(f.db)
	editor, lead := adminUser(t, f, "editor-fw"), adminUser(t, f, "lead-fw")
	pid, err := s.CreateProject(ctx, lead, "北京师范大学", "中国语言文学", "808", "中国语言文学基础", []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	load := func(ctx context.Context, book, edition, revision uint64) ([]rules.TextbookSection, error) {
		r, err := books.GetRevision(ctx, book, edition, revision)
		return r.Sections, err
	}
	// 建一份资料并发布原文（索引由资料库测试覆盖，这里直接标为已发布）。
	material := func(key, kind, title string, secs []rules.TextbookSection, link bool) uint64 {
		t.Helper()
		b, err := books.Create(ctx, lead, key, rules.TextbookMetadata{Title: title, Author: "编者", Kind: kind, Discipline: "文学"},
			rules.TextbookEditionInput{Edition: "2026", Authorized: true, LicenseNote: "测试授权"})
		if err != nil {
			t.Fatal(err)
		}
		e := b.Editions[0]
		r, err := books.NewRevision(ctx, b.ID, e.ID, lead, key+"-rev")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := books.SaveRevision(ctx, b.ID, e.ID, r.ID, lead, r.Version, secs, true); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec("UPDATE textbook_editions SET status='published', current_revision_id=?, current_generation_id=1 WHERE id=?", r.ID, e.ID); err != nil {
			t.Fatal(err)
		}
		if link {
			if _, err := f.db.Exec("INSERT INTO official_project_textbooks (project_id, edition_id, created_at) VALUES (?, ?, NOW(3))", pid, e.ID); err != nil {
				t.Fatal(err)
			}
		}
		return e.ID
	}
	syllabus := material("fw-syllabus", rules.MaterialSyllabus, "北师大808考纲", []rules.TextbookSection{{Heading: "文学理论", Text: "一、文学活动：掌握文学活动的四要素。\n二、文学作品：掌握意境、典型、意象的内涵。"}}, true)
	book := material("fw-textbook", rules.MaterialTextbook, "文学理论教程", []rules.TextbookSection{{Heading: "第一章 文学活动", Text: "正文很长……"}, {Heading: "第八章 文学意境", Text: "正文很长……"}}, true)
	exp := material("fw-experience", rules.MaterialExperience, "808 上岸经验", []rules.TextbookSection{{Heading: "名词解释", Text: "先下定义再举例。"}}, true)
	unlinked := material("fw-unlinked", rules.MaterialSyllabus, "别校考纲", []rules.TextbookSection{{Heading: "古代汉语", Text: "掌握文字、词汇、语法的基础知识。"}}, false)

	for _, id := range []uint64{syllabus, book} {
		file, err := s.CreateFrameworkImport(ctx, editor, pid, id, load)
		if err != nil {
			t.Fatal(err)
		}
		d, err := s.ImportItems(ctx, pid, file.ID)
		if err != nil {
			t.Fatal(err)
		}
		if d.File.Kind != "catalog" {
			t.Fatalf("按参考书目录解析：%+v", d.File)
		}
		if id == book {
			// 模拟模型从纯标题里拆不出知识点，这里只看交给它的文字；考纲那份走完整流程。
			if d.Text != "第一章 文学活动\n第八章 文学意境" {
				t.Errorf("教材只用章节标题：%q", d.Text)
			}
			continue
		}
		if d.File.Status != "parsed" || len(d.Items) == 0 || d.Items[0].ItemType != "knowledge_point" {
			t.Fatalf("知识框架进核对区：%+v %d", d.File, len(d.Items))
		}
	}
	if _, err := s.CreateFrameworkImport(ctx, editor, pid, exp, load); kind(err) != apperr.BadRequest {
		t.Errorf("经验总结不能生成：%v", err)
	}
	if _, err := s.CreateFrameworkImport(ctx, editor, pid, unlinked, load); kind(err) != apperr.NotFound {
		t.Errorf("没关联的资料：%v", err)
	}
	if _, err := s.CreateFrameworkImport(ctx, editor, other0(t, s, lead), syllabus, load); kind(err) != apperr.NotFound {
		t.Errorf("别的项目：%v", err)
	}
}
