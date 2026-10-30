package textbook

import (
	"context"
	"peetraining-server/internal/apperr"
	"testing"
)

func TestLegacyMigrationDryRunIdempotencyAndReconciliation(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO official_projects(id,school,major,subject_code,subject_name,editor_ids)VALUES(1,'测试院校','文学','801','文学','[]')`,
		`INSERT INTO official_references(id,project_id,title,kind,license_note,segments,chars)VALUES(1,1,'同名教材','textbook','旧授权说明',2,8),(2,1,'同名教材','textbook','旧授权说明',0,0)`,
		`INSERT INTO official_reference_segments(reference_id,seq,heading,text)VALUES(1,2,'第一章','完整原文一。'),(1,7,'第二章','完整原文二。')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := s.MigrateLegacyReferences(ctx, 0, false)
	if err != nil || len(preview) != 2 || preview[0].Status != "would_create" {
		t.Fatal(preview, err)
	}
	var n int
	if err = db.QueryRow("SELECT COUNT(*) FROM textbooks").Scan(&n); err != nil || n != 0 {
		t.Fatal("dry-run wrote data", n, err)
	}
	rows, err := s.MigrateLegacyReferences(ctx, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].BookID == rows[1].BookID || rows[0].SourceHash != preview[0].SourceHash {
		t.Fatal("same titles merged or source changed", rows)
	}
	b, err := s.Get(ctx, rows[0].BookID)
	if err != nil {
		t.Fatal(err)
	}
	e := b.Editions[0]
	if e.Authorized || e.Status != "unpublished" || e.CurrentGenerationID.Valid || e.CurrentRevisionID.Valid || e.LicenseNote != "旧授权说明" {
		t.Fatal("unsafe migration state", e)
	}
	r, err := s.GetRevision(ctx, b.ID, e.ID, rows[0].RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "draft" || len(r.Sections) != 2 || r.Sections[0].Text != "完整原文一。" || r.Sections[1].Heading != "第二章" || !r.Sections[0].NeedsReview {
		t.Fatal(r)
	}
	// 不可用未核实的迁移版次发布。
	if _, err = s.SaveRevision(ctx, b.ID, e.ID, r.ID, 1, r.Version, r.Sections, true); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("review bypass", err)
	}
	again, err := s.MigrateLegacyReferences(ctx, 1, true)
	if err != nil || again[0].Status != "already_migrated" || again[0].BookID != rows[0].BookID {
		t.Fatal(again, err)
	}
	if _, err = db.Exec("UPDATE official_reference_segments SET text='修改后的旧原文' WHERE reference_id=1 AND seq=2"); err != nil {
		t.Fatal(err)
	}
	changed, err := s.MigrateLegacyReferences(ctx, 1, true)
	if err != nil || changed[0].Status != "source_changed" {
		t.Fatal(changed, err)
	}
	r, err = s.GetRevision(ctx, b.ID, e.ID, r.ID)
	if err != nil || r.Sections[0].Text != "完整原文一。" {
		t.Fatal("overwrote migrated draft", err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM official_references").Scan(&n); err != nil || n != 2 {
		t.Fatal("legacy deleted", n, err)
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM textbooks").Scan(&n); err != nil || n != 2 {
		t.Fatal("migration duplicated", n, err)
	}
}
