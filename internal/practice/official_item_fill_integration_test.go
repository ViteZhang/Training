package practice_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/official"
)

type itemFillQueue struct{ tasks []*asynq.Task }

func (q *itemFillQueue) EnqueueContext(_ context.Context, t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, t)
	return &asynq.TaskInfo{}, nil
}

// TestOfficialImportItemFillQueued：单题「生成答案 / 重新生成」放到 Worker 里执行（同步会被反向代理按超时断开）。
// 点了立即返回生成中的条目；生成中不能再点、不能接受；Worker 跑完写回答案，重复执行没有副作用；失败原因记在条目上。
func TestOfficialImportItemFillQueued(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	engine := ai.NewEngine(ai.Config{Queries: dbq.New(f.db), UseMock: true})
	s := official.New(official.Deps{DB: f.db, AI: engine, Now: time.Now})
	q := &itemFillQueue{}
	editor, lead := adminUser(t, f, "editor-itemfill"), adminUser(t, f, "lead-itemfill")
	pid, err := s.CreateProject(ctx, lead, "北京师范大学", "中国语言文学", "708", "中国语言文学综合", []uint64{editor})
	if err != nil {
		t.Fatal(err)
	}
	csv := "题型,分值,题干,参考答案,采分点\n简答,10,简述意境的内涵,,\n论述,30,论述文学的本质,,\n"
	file, err := s.CreateImportFile(ctx, editor, pid, official.ImportFileInput{Kind: "table", Format: "csv", Text: csv})
	if err != nil {
		t.Fatal(err)
	}
	s.ConfigureImports(official.ImportDeps{Queue: q, NewItemFillTask: jobs.NewOfficialImportItemFillTask})
	items := func() map[string]official.ImportItem {
		t.Helper()
		d, err := s.ImportItems(ctx, pid, file.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]official.ImportItem{}
		for _, it := range d.Items {
			var p official.QuestionPayload
			_ = json.Unmarshal(it.Payload, &p)
			out[p.Stem] = it
		}
		return out
	}
	pl := func(it official.ImportItem) official.QuestionPayload {
		var p official.QuestionPayload
		_ = json.Unmarshal(it.Payload, &p)
		return p
	}
	run := func() {
		t.Helper()
		if len(q.tasks) == 0 {
			t.Fatal("没有排上任务")
		}
		task := q.tasks[0]
		q.tasks = q.tasks[1:]
		if task.Type() != jobs.TypeOfficialImportItemFill {
			t.Fatalf("任务类型：%s", task.Type())
		}
		var p struct {
			ID         uint64 `json:"id"`
			Regenerate bool   `json:"regenerate"`
		}
		if err := json.Unmarshal(task.Payload(), &p); err != nil {
			t.Fatal(err)
		}
		if err := s.ProcessImportItemFill(ctx, p.ID, p.Regenerate); err != nil {
			t.Fatal(err)
		}
	}
	got := items()
	gen, noScore := got["简述意境的内涵"], got["论述文学的本质"]
	// 论述题去掉分值（如 PDF 解析没认出分值），同项目也没有别的论述题可参考。
	if _, err := f.db.Exec("UPDATE official_import_items SET payload = JSON_REMOVE(payload, '$.score') WHERE id = ?", noScore.ID); err != nil {
		t.Fatal(err)
	}

	// 点「生成答案」：立即返回生成中的条目，答案还没写。
	it, err := s.FillImportItem(ctx, pid, gen.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !it.Filling || !it.NeedsFill || pl(it).Answer != "" || len(q.tasks) != 1 {
		t.Fatalf("排队：%+v 任务 %d", it, len(q.tasks))
	}
	if _, err := s.FillImportItem(ctx, pid, gen.ID); kind(err) != apperr.Conflict {
		t.Errorf("生成中再点：%v", err)
	}
	if _, err := s.ReviewImportItem(ctx, editor, pid, gen.ID, "accept", nil); kind(err) != apperr.Conflict {
		t.Errorf("生成中接受：%v", err)
	}
	run()
	done := items()["简述意境的内涵"]
	if done.Filling || done.NeedsFill || pl(done).Answer == "" || !done.AnswerAI || pl(done).FillQueuedAt != nil {
		t.Fatalf("生成完：%+v %+v", done, pl(done))
	}
	// 重复执行（任务被投递两次）不再生成。
	if err := s.ProcessImportItemFill(ctx, gen.ID, false); err != nil {
		t.Fatal(err)
	}
	if pl(items()["简述意境的内涵"]).AnswerSnapshotID != pl(done).AnswerSnapshotID {
		t.Error("重复执行又生成了一次")
	}

	// 重新生成：同样排队，跑完换成新的答案。
	re, err := s.RegenerateImportItem(ctx, pid, gen.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !re.Filling || pl(re).Answer != pl(done).Answer {
		t.Fatalf("重新生成排队：%+v", re)
	}
	run()
	again := items()["简述意境的内涵"]
	if again.Filling || pl(again).Regenerate || pl(again).AnswerSnapshotID == pl(done).AnswerSnapshotID || pl(again).Answer == "" {
		t.Fatalf("重新生成后：%+v", pl(again))
	}

	// 没有分值、也没有同题型可参考：失败原因记在条目上，不再显示生成中，可以再点。
	if _, err := s.FillImportItem(ctx, pid, noScore.ID); err != nil {
		t.Fatal(err)
	}
	run()
	failed := items()["论述文学的本质"]
	if failed.Filling || !failed.NeedsFill || !strings.Contains(failed.FillError, "分值") || pl(failed).FillQueuedAt != nil {
		t.Fatalf("失败：%+v", failed)
	}
	if _, err := s.FillImportItem(ctx, pid, noScore.ID); err != nil {
		t.Fatalf("失败后可以再点：%v", err)
	}

	// 接受：排队标记不进草稿。
	id, err := s.ReviewImportItem(ctx, editor, pid, gen.ID, "accept", nil)
	if err != nil {
		t.Fatal(err)
	}
	var draft string
	if err := f.db.QueryRow("SELECT CAST(payload AS CHAR) FROM official_drafts WHERE id = ?", id).Scan(&draft); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(draft, "fill_queued_at") {
		t.Errorf("草稿里不该有排队标记：%s", draft)
	}
}
