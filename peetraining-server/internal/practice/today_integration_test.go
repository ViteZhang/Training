package practice_test

import (
	"context"
	"testing"

	"peetraining-server/internal/practice"
)

// 今日训练一个按钮做到底：题和背诵一段接一段，做过的题不再出现，答完直接退出（没结算）也不会回到旧题。
func TestContinueTodayWalksPlanWithoutRepeats(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)

	pl, err := f.plan.Today(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	total := len(pl.Items)
	first, err := f.pr.ContinueToday(ctx, uid, sid, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Step != practice.StepPractice || first.Remaining != total || first.SubjectID != sid {
		t.Fatalf("先做这门课的题：%+v（计划 %d 项）", first, total)
	}
	s1, _ := f.pr.Get(ctx, uid, first.SessionID)
	if len(s1.Questions) < 2 {
		t.Fatalf("计划里的题：%+v", s1.Questions)
	}
	// 只做第一道就结束这一组：再继续时新的一组不含做过的题。
	seen := map[uint64]bool{s1.Questions[0].ID: true}
	if _, err := f.pr.Submit(ctx, uid, s1.ID, practice.AttemptInput{QuestionID: s1.Questions[0].ID, Key: nextKey(), Revealed: true}); err != nil {
		t.Fatal(err)
	}
	if again, _ := f.pr.ContinueToday(ctx, uid, 0, false); again.SessionID != s1.ID || again.Remaining != total-1 {
		t.Errorf("还有题没答时从断点继续同一组：%+v", again)
	}
	if _, err := f.pr.Finish(ctx, uid, s1.ID); err != nil {
		t.Fatal(err)
	}

	recited := false
	for i := 0; ; i++ {
		if i > 20 {
			t.Fatal("今日训练一直做不完")
		}
		st, err := f.pr.ContinueToday(ctx, uid, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		if st.Step == practice.StepDone {
			break
		}
		if st.Step == practice.StepPractice {
			s, err := f.pr.Get(ctx, uid, st.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			if s.ID == s1.ID || len(s.Questions) == 0 {
				t.Fatalf("应新开一组：%+v", st)
			}
			for _, q := range s.Questions {
				if seen[q.ID] {
					t.Errorf("做过的题又出现了：%d %s", q.ID, q.Stem)
				}
				seen[q.ID] = true
				if _, err := f.pr.Submit(ctx, uid, s.ID, practice.AttemptInput{QuestionID: q.ID, Key: nextKey(), Revealed: true}); err != nil {
					t.Fatal(err)
				}
			}
			// 答完直接退出，不点「完成本组」：训练首页不再给这组的断点，下一次继续时它已结算。
			home, err := f.pr.Home(ctx, uid, sid)
			if err != nil {
				t.Fatal(err)
			}
			if home.TodaySession != 0 {
				t.Errorf("答完的一组不应还是断点：%+v", home)
			}
			var status string
			_ = f.db.QueryRow("SELECT status FROM practice_sessions WHERE id = ?", s.ID).Scan(&status)
			if status != "finished" {
				t.Errorf("答完的一组应已结算：%s", status)
			}
			continue
		}
		recited = true
		r, err := f.pr.GetRecite(ctx, uid, st.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		todo := 0
		for _, it := range r.Items {
			if it.Result != "" {
				continue
			}
			todo++
			if _, err := f.pr.RecordRecite(ctx, uid, r.ID, practice.ReciteInput{KPID: it.KPID, Mode: "cloze", Result: "remembered", Key: nextKey()}); err != nil {
				t.Fatal(err)
			}
		}
		if todo == 0 {
			t.Fatalf("不应给一轮已经背完的：%+v", r)
		}
		// 背完也直接退出，不结算。
	}
	if !recited {
		t.Error("题做完后应接着背诵")
	}
	if _, err := f.pr.Create(ctx, uid, practice.CreateInput{SubjectID: sid, Kind: practice.KindDaily}); err == nil {
		t.Error("题都做完后再开今日训练应提示已做完，而不是重出一遍")
	}
	// 今天背过的不再排进新的一轮。
	done, _ := f.plan.Today(ctx, uid)
	for i, it := range done.Items {
		if it.QuestionID != 0 && !done.Done[i] {
			t.Errorf("计划里的题应都做完：%+v", it)
		}
	}
}
