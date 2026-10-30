package textbook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/hibiken/asynq"
	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	cloudai "peetraining-server/internal/cloud/ai"
	"peetraining-server/internal/cloud/oss"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/params"
	"peetraining-server/internal/rules"
)

type importQueue struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (q *importQueue) EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls++
	if q.fail {
		return nil, errors.New("offline")
	}
	return &asynq.TaskInfo{}, nil
}

type importRenderer struct{}

func (importRenderer) RenderAllPages(ctx context.Context, data []byte, max int, cb func(int, int, []byte) error) error {
	pages := strings.Split(string(data), "\f")
	for i, p := range pages {
		if err := cb(len(pages), i+1, []byte(fmt.Sprintf("%d|%s", i+1, p))); err != nil {
			return err
		}
	}
	return nil
}

type importModel struct {
	mu         sync.Mutex
	calls      map[int]int
	failSecond bool
}

func (m *importModel) Complete(_ context.Context, r cloudai.Request) (cloudai.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var page int
	_, _ = fmt.Sscanf(string(r.Messages[1].Images[0].Data), "%d|", &page)
	m.calls[page]++
	if page == 2 && m.failSecond {
		return cloudai.Response{}, errors.New("provider failed")
	}
	raw, _ := json.Marshal(ai.TextbookPageOut{PageNo: page, Blocks: []ai.TextbookBlock{{Heading: "教材章节", Text: fmt.Sprintf("教材第%d页原文", page), Uncertain: page == 2}}})
	return cloudai.Response{Content: string(raw)}, nil
}
func importFile(name, format, contents string) rules.TextbookImportFile {
	h := sha256.Sum256([]byte(contents))
	return rules.TextbookImportFile{Name: name, Format: format, Size: int64(len(contents)), SHA256: hex.EncodeToString(h[:])}
}
func TestImportPDFCheckpointAndAdoption(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	storage := oss.NewMock()
	queue := &importQueue{fail: true}
	model := &importModel{calls: map[int]int{}, failSecond: true}
	s.ConfigureImports(ImportDeps{OSS: storage, AI: ai.NewEngine(ai.Config{Client: model}), Renderer: importRenderer{}, Queue: queue, Params: params.New(dbq.New(db))})
	b := mustBook(t, s, "import-book-key")
	ed := b.Editions[0].ID
	r, err := s.NewRevision(ctx, b.ID, ed, 1, "import-revision")
	if err != nil {
		t.Fatal(err)
	}
	content := "文字PDF第1页\f扫描PDF第2页"
	file := importFile("教材.pdf", "pdf", content)
	j, _, err := s.CreateImport(ctx, b.ID, ed, r.ID, 1, "import-request", file)
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := s.CreateImport(ctx, b.ID, ed, r.ID, 1, "import-request", file)
	if err != nil || j.ID != again.ID {
		t.Fatal("上传申请不幂等", err)
	}
	if err = s.QueueImport(ctx, b.ID, ed, r.ID, j.ID, "commit-request", false); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("未上传不应入队", err)
	}
	storage.Seed(uploadKey(j.ID), []byte(content))
	if err = s.QueueImport(ctx, b.ID, ed, r.ID, j.ID, "commit-request", false); err != nil {
		t.Fatal("入队故障应保留排队记录", err)
	}
	queue.fail = false
	if err = s.RecoverImports(ctx); err != nil || queue.calls != 2 {
		t.Fatal("恢复入队", err, queue.calls)
	}
	if err = s.ProcessImport(ctx, j.ID); err == nil {
		t.Fatal("第二页失败应保留失败状态")
	}
	pages, err := dbq.New(db).ListTextbookImportPages(ctx, j.ID)
	if err != nil || len(pages) != 1 {
		t.Fatal("逐页检查点丢失", err)
	}
	model.failSecond = false
	if err = s.ProcessImport(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	if model.calls[1] != 1 || model.calls[2] != 2 {
		t.Fatalf("重试重复识别已成功页: %v", model.calls)
	}
	// 客户端上传键后来被覆盖也不影响服务端封存原件和检查点。
	storage.Seed(uploadKey(j.ID), []byte("替换文件"))
	if err = s.ProcessImport(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetRevision(ctx, b.ID, ed, r.ID)
	if err != nil || len(current.Sections) != 0 {
		t.Fatal("识别擅自覆盖了草稿", err)
	}
	applied, err := s.ApplyImport(ctx, b.ID, ed, r.ID, j.ID, 1, r.Version, "append")
	if err != nil || len(applied.Sections) != 2 || !applied.Sections[1].NeedsReview {
		t.Fatal("采用缺少原文或不确定标记", err)
	}
	if _, err = s.ApplyImport(ctx, b.ID, ed, r.ID, j.ID, 1, r.Version, "append"); err != nil {
		t.Fatal("重复采用应幂等", err)
	}
	if _, err = s.ApplyImport(ctx, b.ID, ed, r.ID, j.ID, 1, r.Version, "replace"); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatal("异参重放应拒绝", err)
	}
	if _, err = s.SaveRevision(ctx, b.ID, ed, r.ID, 1, applied.Version, applied.Sections, true); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("不确定内容未核对不应校对", err)
	}
	applied.Sections[1].NeedsReview = false
	if _, err = s.SaveRevision(ctx, b.ID, ed, r.ID, 1, applied.Version, applied.Sections, true); err != nil {
		t.Fatal(err)
	}
	other := mustBook(t, s, "other-import-book")
	if _, _, err = s.ImportPage(ctx, other.ID, ed, r.ID, j.ID, 1); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("跨教材访问应404", err)
	}
}
func TestImportChecksumBudgetAndVersionConflict(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	storage := oss.NewMock()
	model := &importModel{calls: map[int]int{}}
	s.ConfigureImports(ImportDeps{OSS: storage, AI: ai.NewEngine(ai.Config{Client: model}), Renderer: importRenderer{}, Queue: &importQueue{}, Params: params.New(dbq.New(db))})
	b := mustBook(t, s, "budget-book-key")
	ed := b.Editions[0].ID
	r, err := s.NewRevision(ctx, b.ID, ed, 1, "budget-revision")
	if err != nil {
		t.Fatal(err)
	}
	content := "第一部分"
	j, _, err := s.CreateImport(ctx, b.ID, ed, r.ID, 1, "budget-import", importFile("原文.txt", "txt", content))
	if err != nil {
		t.Fatal(err)
	}
	storage.Seed(uploadKey(j.ID), []byte("错误文件"))
	if err = s.ProcessImport(ctx, j.ID); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("上传中不能识别", err)
	}
	storage.Seed(uploadKey(j.ID), []byte(content))
	if err = s.QueueImport(ctx, b.ID, ed, r.ID, j.ID, "budget-commit", false); err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessImport(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := s.SaveRevision(ctx, b.ID, ed, r.ID, 1, r.Version, []rules.TextbookSection{{Text: "人工补充的正文"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyImport(ctx, b.ID, ed, r.ID, j.ID, 1, r.Version, "replace"); !apperr.IsKind(err, apperr.Conflict) {
		t.Fatal("旧版本采用应冲突", err)
	}
	current, _ := s.GetRevision(ctx, b.ID, ed, r.ID)
	if current.Sections[0].Text != "人工补充的正文" {
		t.Fatal("人工原文被覆盖")
	}
	if _, err = s.ApplyImport(ctx, b.ID, ed, r.ID, j.ID, 1, saved.Version, "append"); err != nil {
		t.Fatal(err)
	}
	// 预算是共享事务计数，同一天重复尝试也不能越过页数上限。
	l, _ := s.ImportSettings(ctx)
	l.MaxDailyPDFPages = 1
	if err = s.reservePDFPage(ctx, l); err != nil {
		t.Fatal(err)
	}
	if err = s.reservePDFPage(ctx, l); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal("预算未限制", err)
	}
}
