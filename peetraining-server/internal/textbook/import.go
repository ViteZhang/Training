package textbook

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hibiken/asynq"
	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/cloud/ocr"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/extract"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/params"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
)

type PageRenderer interface {
	RenderAllPages(context.Context, []byte, int, func(total, page int, jpeg []byte) error) error
}
type Enqueuer interface {
	EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}
type ImportDeps struct {
	OSS      oss.Store
	AI       *ai.Engine
	Renderer PageRenderer
	Queue    Enqueuer
	Params   *params.Store
}

func (s *Service) ConfigureImports(d ImportDeps) { s.imports = d }
func (s *Service) ImportSettings(ctx context.Context) (rules.TextbookImportLimits, error) {
	var l rules.TextbookImportLimits
	err := s.imports.Params.Get(ctx, "textbook_import_limits", &l)
	if err == nil {
		err = rules.ValidateTextbookImportLimits(l)
	}
	return l, err
}
func uploadKey(id uint64) string { return fmt.Sprintf("textbooks/imports/%d/upload", id) }
func sourceKey(id uint64) string { return fmt.Sprintf("textbooks/imports/%d/source", id) }
func imageKey(id uint64, page int) string {
	return fmt.Sprintf("textbooks/imports/%d/pages/%d.jpg", id, page)
}
func contentType(format string) string {
	switch format {
	case "pdf":
		return "application/pdf"
	case "docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "text/plain"
	}
}
func (s *Service) CreateImport(ctx context.Context, book, edition, revision, actor uint64, key string, f rules.TextbookImportFile) (dbq.TextbookImport, oss.Presigned, error) {
	var out dbq.TextbookImport
	l, err := s.ImportSettings(ctx)
	if err != nil {
		return out, oss.Presigned{}, err
	}
	if err = rules.ValidateTextbookImport(f, l); err != nil {
		return out, oss.Presigned{}, err
	}
	if err = rules.ValidateTextbookRequestKey(key); err != nil {
		return out, oss.Presigned{}, err
	}
	hash, _, err := rules.TextbookHash(f)
	if err != nil {
		return out, oss.Presigned{}, err
	}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if _, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: edition, BookID: book}); err != nil {
			return err
		}
		r, err := q.LockTextbookRevision(ctx, dbq.LockTextbookRevisionParams{ID: revision, EditionID: edition})
		if err != nil {
			return err
		}
		if r.Status == "published" {
			return versionConflict()
		}
		old, err := q.GetTextbookImportRequest(ctx, dbq.GetTextbookImportRequestParams{RevisionID: revision, CreatedBy: actor, RequestKey: key})
		if err == nil {
			if old.RequestHash != hash {
				return requestConflict()
			}
			out = old
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		count, err := q.CountTextbookImports(ctx, revision)
		if err != nil {
			return err
		}
		if count >= int64(l.MaxImportsPerRevision) {
			return apperr.New(apperr.BadRequest, "本修订导入文件数已达上限，请新建修订")
		}
		now := s.now().UTC()
		id, err := q.CreateTextbookImport(ctx, dbq.CreateTextbookImportParams{RevisionID: revision, FileName: f.Name, Format: f.Format, SizeBytes: f.Size, Sha256: f.SHA256, CreatedBy: actor, RequestKey: key, RequestHash: hash, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			return err
		}
		out, err = q.GetTextbookImport(ctx, dbq.GetTextbookImportParams{ID: uint64(id), RevisionID: revision})
		return err
	})
	if err != nil {
		return out, oss.Presigned{}, mapError(err)
	}
	signed, err := s.imports.OSS.PresignPut(ctx, uploadKey(out.ID), contentType(f.Format), f.Size, 30*time.Minute)
	return out, signed, err
}
func (s *Service) ListImports(ctx context.Context, book, edition, revision uint64) ([]dbq.ListTextbookImportsRow, error) {
	if err := s.checkRevision(ctx, book, edition, revision); err != nil {
		return nil, err
	}
	return dbq.New(s.db).ListTextbookImports(ctx, revision)
}
func (s *Service) scopedImport(ctx context.Context, book, edition, revision, id uint64) (dbq.TextbookImport, error) {
	if err := s.checkRevision(ctx, book, edition, revision); err != nil {
		return dbq.TextbookImport{}, err
	}
	j, err := dbq.New(s.db).GetTextbookImport(ctx, dbq.GetTextbookImportParams{ID: id, RevisionID: revision})
	return j, mapError(err)
}
func (s *Service) QueueImport(ctx context.Context, book, edition, revision, id uint64, key string, retry bool) error {
	if err := rules.ValidateTextbookRequestKey(key); err != nil {
		return err
	}
	j, err := s.scopedImport(ctx, book, edition, revision, id)
	if err != nil {
		return err
	}
	if !retry && j.Status == "uploading" {
		head, err := s.imports.OSS.Head(ctx, uploadKey(id))
		if err != nil {
			return err
		}
		if !head.Exists || head.Size != j.SizeBytes {
			return apperr.New(apperr.BadRequest, "文件尚未完整上传，请重新上传")
		}
	}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		r, err := q.GetTextbookRevision(ctx, dbq.GetTextbookRevisionParams{ID: revision, EditionID: edition})
		if err != nil {
			return err
		}
		if r.Status == "published" {
			return versionConflict()
		}
		j, err = q.LockTextbookImport(ctx, dbq.LockTextbookImportParams{ID: id, RevisionID: revision})
		if err != nil {
			return err
		}
		if retry && j.RetryKey == key {
			return nil
		}
		if j.Status == "ready" || j.Status == "applied" || j.Status == "queued" || j.Status == "processing" {
			return nil
		}
		if retry && j.Status != "failed" || !retry && j.Status != "uploading" {
			return versionConflict()
		}
		if j.Attempt >= 5 {
			return apperr.New(apperr.BadRequest, "任务重试已达上限，请检查原件后新建修订")
		}
		if err = q.QueueTextbookImport(ctx, dbq.QueueTextbookImportParams{RetryKey: key, UpdatedAt: s.now().UTC(), ID: id}); err != nil {
			return err
		}
		j.Attempt++
		j.Status = "queued"
		return nil
	})
	if err != nil {
		return mapError(err)
	}
	if j.Status == "queued" || j.Status == "processing" {
		_ = s.enqueueImport(ctx, id, j.Attempt)
	}
	return nil
}
func (s *Service) enqueueImport(ctx context.Context, id uint64, attempt uint32) error {
	task, err := jobs.NewTextbookImportTask(id, attempt)
	if err != nil {
		return err
	}
	_, err = s.imports.Queue.EnqueueContext(ctx, task)
	if errors.Is(err, asynq.ErrTaskIDConflict) || errors.Is(err, asynq.ErrDuplicateTask) {
		return nil
	}
	return err
}
func (s *Service) RecoverImports(ctx context.Context) error {
	rows, err := dbq.New(s.db).RecoverTextbookImports(ctx, s.now().UTC().Add(-15*time.Minute))
	if err != nil {
		return err
	}
	for _, j := range rows {
		if err = s.enqueueImport(ctx, j.ID, j.Attempt); err != nil {
			return err
		}
	}
	return nil
}

// lockImport 用独立连接持有 MySQL 会话锁。全局两个槽位，跨 API/Worker 进程限制大模型导入并发。
func (s *Service) lockImport(ctx context.Context, id uint64, slots int) (func(), error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	names := []string{}
	release := func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for i := len(names) - 1; i >= 0; i-- {
			_, _ = conn.ExecContext(c, "SELECT RELEASE_LOCK(?)", names[i])
		}
		_ = conn.Close()
	}
	acquire := func(name string) (bool, error) {
		var got sql.NullInt64
		err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", name).Scan(&got)
		if err != nil {
			return false, err
		}
		if got.Valid && got.Int64 == 1 {
			names = append(names, name)
			return true, nil
		}
		return false, nil
	}
	ok, err := acquire(fmt.Sprintf("textbook-import-%d", id))
	if err != nil || !ok {
		release()
		if err != nil {
			return nil, err
		}
		return nil, apperr.New(apperr.Conflict, "任务正在处理")
	}
	for i := 0; i < slots; i++ {
		ok, err = acquire(fmt.Sprintf("textbook-import-slot-%d", i))
		if err != nil {
			release()
			return nil, err
		}
		if ok {
			return release, nil
		}
	}
	release()
	return nil, errors.New("textbook import concurrency limit")
}
func (s *Service) reservePDFPage(ctx context.Context, l rules.TextbookImportLimits) error {
	return store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		day := s.now().In(jobs.Shanghai)
		date := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
		if err := q.EnsureTextbookDailyBudget(ctx, date); err != nil {
			return err
		}
		used, err := q.LockTextbookDailyBudget(ctx, date)
		if err != nil {
			return err
		}
		if used >= uint32(l.MaxDailyPDFPages) {
			return apperr.New(apperr.BadRequest, "今日教材 PDF 识别页数预算已达上限，可调整预算或次日重试")
		}
		return q.AddTextbookDailyBudget(ctx, dbq.AddTextbookDailyBudgetParams{PdfPages: 1, BudgetDay: date})
	})
}
func (s *Service) readImport(ctx context.Context, j dbq.TextbookImport, l rules.TextbookImportLimits) ([]byte, error) {
	key := sourceKey(j.ID)
	if !j.Sealed {
		key = uploadKey(j.ID)
	}
	rc, err := s.imports.OSS.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, (int64(l.MaxFileMB)<<20)+1))
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != j.SizeBytes || hex.EncodeToString(sum[:]) != j.Sha256 {
		return nil, apperr.New(apperr.BadRequest, "文件大小或校验值发生变化，请重新上传")
	}
	if !j.Sealed {
		if err = s.imports.OSS.Put(ctx, sourceKey(j.ID), data, contentType(j.Format)); err != nil {
			return nil, err
		}
		if err = dbq.New(s.db).SealTextbookImport(ctx, dbq.SealTextbookImportParams{UpdatedAt: s.now().UTC(), ID: j.ID}); err != nil {
			return nil, err
		}
	}
	return data, nil
}

// ProcessImport 逐页持久化检查点。失败不会把已完成页或人工草稿清空。
func (s *Service) ProcessImport(ctx context.Context, id uint64) (err error) {
	l, err := s.ImportSettings(ctx)
	if err != nil {
		return err
	}
	release, err := s.lockImport(ctx, id, l.MaxConcurrentImports)
	if err != nil {
		return err
	}
	defer release()
	q := dbq.New(s.db)
	j, err := q.GetTextbookImportByID(ctx, id)
	if err != nil {
		return mapError(err)
	}
	if j.Status == "ready" || j.Status == "applied" {
		return nil
	}
	if j.Status == "uploading" {
		return apperr.New(apperr.BadRequest, "文件尚未提交")
	}
	defer func() {
		if err != nil {
			safe, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			message := "识别失败，已完成页面保留，可重试"
			if e, ok := apperr.As(err); ok && e.Kind == apperr.BadRequest {
				message = e.Message
			}
			_ = q.UpdateTextbookImportState(safe, dbq.UpdateTextbookImportStateParams{Status: "failed", FailureMessage: message, UpdatedAt: s.now().UTC(), ID: id})
		}
	}()
	if err = q.UpdateTextbookImportState(ctx, dbq.UpdateTextbookImportStateParams{Status: "processing", UpdatedAt: s.now().UTC(), ID: id}); err != nil {
		return err
	}
	data, err := s.readImport(ctx, j, l)
	if err != nil {
		return err
	}
	savePage := func(total, page int, out ai.TextbookPageOut, meta ai.Meta) error {
		_, payload, err := rules.TextbookHash(out)
		if err != nil {
			return err
		}
		uncertain := false
		for _, b := range out.Blocks {
			uncertain = uncertain || b.Uncertain
		}
		if err = q.SaveTextbookImportPage(ctx, dbq.SaveTextbookImportPageParams{ImportID: id, PageNo: uint32(page), Payload: payload, Uncertain: uncertain, Model: meta.Model, PromptVersion: meta.Version}); err != nil {
			return err
		}
		return q.SetTextbookImportPageCount(ctx, dbq.SetTextbookImportPageCountParams{PageCount: uint32(total), UpdatedAt: s.now().UTC(), ID: id})
	}
	exists := func(page int) (bool, error) {
		_, err := q.GetTextbookImportPage(ctx, dbq.GetTextbookImportPageParams{ImportID: id, PageNo: uint32(page)})
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return err == nil, err
	}
	if j.Format == "pdf" {
		err = s.imports.Renderer.RenderAllPages(ctx, data, l.MaxPages, func(total, page int, image []byte) error {
			if err := q.SetTextbookImportPageCount(ctx, dbq.SetTextbookImportPageCountParams{PageCount: uint32(total), UpdatedAt: s.now().UTC(), ID: id}); err != nil {
				return err
			}
			done, err := exists(page)
			if err != nil || done {
				return err
			}
			if err = s.reservePDFPage(ctx, l); err != nil {
				return err
			}
			if err = s.imports.OSS.Put(ctx, imageKey(id, page), image, "image/jpeg"); err != nil {
				return err
			}
			out, meta, err := ai.RecognizeTextbook.Run(ctx, s.imports.AI, 0, ai.TextbookPageIn{PageNo: page, Image: cloudai.Image{Data: image}})
			if err != nil {
				return err
			}
			return savePage(total, page, out, meta)
		})
		switch {
		case errors.Is(err, ocr.ErrPDFEncrypted):
			return apperr.New(apperr.BadRequest, "PDF 设置了密码，请移除密码后重传")
		case errors.Is(err, ocr.ErrPDFBroken):
			return apperr.New(apperr.BadRequest, "PDF 损坏或无法打开")
		case errors.Is(err, ocr.ErrPDFTooManyPages):
			return apperr.New(apperr.BadRequest, "PDF 页数超过教材导入上限，请分册导入")
		case err != nil:
			return err
		}
	} else {
		var res extract.Result
		if j.Format == "docx" {
			res, err = extract.Docx(data, 1500)
		} else {
			if !utf8.Valid(data) {
				return apperr.New(apperr.BadRequest, "文本文件需使用 UTF-8 编码")
			}
			res = extract.Text(strings.TrimPrefix(string(data), "\ufeff"), 1500)
		}
		if err != nil {
			if e, ok := extract.AsUserError(err); ok {
				return apperr.New(apperr.BadRequest, "Word 原文无法读取，请检查格式或转换为 PDF 后导入").Wrap(e)
			}
			return err
		}
		if len(res.Pages) < 1 || len(res.Pages) > l.MaxPages {
			return apperr.New(apperr.BadRequest, "文本为空或超过导入上限，请分章导入")
		}
		for _, p := range res.Pages {
			done, err := exists(p.No)
			if err != nil {
				return err
			}
			if done {
				continue
			}
			out := ai.TextbookPageOut{PageNo: p.No, Blocks: []ai.TextbookBlock{{Text: p.Text}}}
			if err = savePage(len(res.Pages), p.No, out, ai.Meta{Model: "deterministic", Version: "docx-text-v1"}); err != nil {
				return err
			}
		}
	}
	pages, err := q.ListTextbookImportPages(ctx, id)
	if err != nil {
		return err
	}
	j, err = q.GetTextbookImportByID(ctx, id)
	if err != nil {
		return err
	}
	if len(pages) == 0 || len(pages) != int(j.PageCount) {
		return errors.New("textbook page checkpoint incomplete")
	}
	for i, p := range pages {
		if p.PageNo != uint32(i+1) {
			return errors.New("textbook page order incomplete")
		}
	}
	return q.UpdateTextbookImportState(ctx, dbq.UpdateTextbookImportStateParams{Status: "ready", UpdatedAt: s.now().UTC(), ID: id})
}
func (s *Service) ImportPage(ctx context.Context, book, edition, revision, id uint64, page int) (ai.TextbookPageOut, string, error) {
	j, err := s.scopedImport(ctx, book, edition, revision, id)
	if err != nil {
		return ai.TextbookPageOut{}, "", err
	}
	p, err := dbq.New(s.db).GetTextbookImportPage(ctx, dbq.GetTextbookImportPageParams{ImportID: id, PageNo: uint32(page)})
	if err != nil {
		return ai.TextbookPageOut{}, "", mapError(err)
	}
	var out ai.TextbookPageOut
	if err = json.Unmarshal(p.Payload, &out); err != nil {
		return out, "", err
	}
	image := ""
	if j.Format == "pdf" {
		signed, err := s.imports.OSS.PresignGet(ctx, imageKey(id, page), "page.jpg", 5*time.Minute)
		if err != nil {
			return out, "", err
		}
		image = signed.URL
	}
	return out, image, nil
}
func (s *Service) ApplyImport(ctx context.Context, book, edition, revision, id, actor, version uint64, mode string) (Revision, error) {
	if mode != "append" && mode != "replace" {
		return Revision{}, apperr.New(apperr.BadRequest, "请选择追加或替换")
	}
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if _, err := q.LockTextbookEdition(ctx, dbq.LockTextbookEditionParams{ID: edition, BookID: book}); err != nil {
			return err
		}
		r, err := q.LockTextbookRevision(ctx, dbq.LockTextbookRevisionParams{ID: revision, EditionID: edition})
		if err != nil {
			return err
		}
		j, err := q.LockTextbookImport(ctx, dbq.LockTextbookImportParams{ID: id, RevisionID: revision})
		if err != nil {
			return err
		}
		if j.Status == "applied" {
			if j.AppliedMode != mode {
				return requestConflict()
			}
			return nil
		}
		if r.Status == "published" || r.Version != version {
			return versionConflict()
		}
		if j.Status != "ready" {
			return apperr.New(apperr.Conflict, "识别尚未完成")
		}
		pages, err := q.ListTextbookImportPages(ctx, id)
		if err != nil {
			return err
		}
		if len(pages) != int(j.PageCount) {
			return versionConflict()
		}
		sections := []rules.TextbookSection{}
		if mode == "append" {
			if err = json.Unmarshal(r.Sections, &sections); err != nil {
				return err
			}
		}
		for _, page := range pages {
			var out ai.TextbookPageOut
			if err = json.Unmarshal(page.Payload, &out); err != nil {
				return err
			}
			for _, b := range out.Blocks {
				p := int(page.PageNo)
				importID := int64(id)
				printed := out.PrintedPage
				sec := rules.TextbookSection{Heading: b.Heading, Text: b.Text, SourcePage: &p, SourceImportID: &importID, NeedsReview: b.Uncertain}
				if printed != "" {
					sec.PrintedPage = &printed
				}
				sections = append(sections, sec)
			}
		}
		sections, err = rules.NormalizeTextbookSections(sections)
		if err != nil {
			return err
		}
		hash, data, err := rules.TextbookHash(sections)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		if _, err = q.SaveTextbookRevision(ctx, dbq.SaveTextbookRevisionParams{Sections: data, ContentHash: hash, Status: "draft", UpdatedAt: now, ID: revision, EditionID: edition, Version: version}); err != nil {
			return err
		}
		return q.ApplyTextbookImport(ctx, dbq.ApplyTextbookImportParams{AppliedMode: mode, UpdatedAt: now, ID: id})
	})
	if err != nil {
		return Revision{}, mapError(err)
	}
	return s.GetRevision(ctx, book, edition, revision)
}

func (s *Service) checkRevision(ctx context.Context, book, edition, revision uint64) error {
	q := dbq.New(s.db)
	if _, err := q.GetTextbookEdition(ctx, dbq.GetTextbookEditionParams{ID: edition, BookID: book}); err != nil {
		return mapError(err)
	}
	_, err := q.GetTextbookRevisionIdentity(ctx, dbq.GetTextbookRevisionIdentityParams{ID: revision, EditionID: edition})
	return mapError(err)
}

func (s *Service) RenewImportUpload(ctx context.Context, book, edition, revision, id uint64) (dbq.TextbookImport, oss.Presigned, error) {
	j, err := s.scopedImport(ctx, book, edition, revision, id)
	if err != nil {
		return j, oss.Presigned{}, err
	}
	r, err := dbq.New(s.db).GetTextbookRevisionIdentity(ctx, dbq.GetTextbookRevisionIdentityParams{ID: revision, EditionID: edition})
	if err != nil {
		return j, oss.Presigned{}, mapError(err)
	}
	if r.Status == "published" || j.Sealed || (j.Status != "uploading" && j.Status != "failed") {
		return j, oss.Presigned{}, versionConflict()
	}
	signed, err := s.imports.OSS.PresignPut(ctx, uploadKey(id), contentType(j.Format), j.SizeBytes, 30*time.Minute)
	return j, signed, err
}
