package practice_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/practice"
	"peetraining-server/internal/rules"
)

func findItem(s practice.ReciteSession, name string) practice.ReciteItem {
	for _, it := range s.Items {
		if it.Name == name {
			return it
		}
	}
	return practice.ReciteItem{}
}

func TestReciteScheduleAndNextDayPlan(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)

	s, err := f.pr.CreateRecite(ctx, uid, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Items) < 3 || s.OralEnabled {
		t.Fatalf("一轮背诵：%+v", s)
	}
	for _, it := range s.Items {
		if it.OriginalText == "" || len(it.Segments) == 0 {
			t.Errorf("每条都要有原文：%+v", it)
		}
		joined := ""
		for _, sg := range it.Segments {
			joined += sg.Text
		}
		if joined != it.OriginalText {
			t.Errorf("挖空切段要能拼回原文：%q vs %q", joined, it.OriginalText)
		}
	}
	again, _ := f.pr.CreateRecite(ctx, uid, sid, 0)
	if again.ID != s.ID {
		t.Error("当天进行中的一轮应继续")
	}

	forgot, kept := s.Items[0], s.Items[1]
	today := rules.DayOf(f.clock)
	key := nextKey()
	r, err := f.pr.RecordRecite(ctx, uid, s.ID, practice.ReciteInput{KPID: forgot.KPID, Mode: "cloze", Result: "forgot", Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if r.Result != "forgot" || r.NextReview != today.AddDays(1) {
		t.Errorf("没记住 1 天后复习：%+v", r)
	}
	if replay, err := f.pr.RecordRecite(ctx, uid, s.ID, practice.ReciteInput{KPID: forgot.KPID, Mode: "cloze", Result: "remembered", Key: key}); err != nil || replay.Result != "forgot" {
		t.Errorf("幂等：%+v %v", replay, err)
	}
	r, err = f.pr.RecordRecite(ctx, uid, s.ID, practice.ReciteInput{KPID: kept.KPID, Mode: "cloze", Result: "remembered", Key: nextKey()})
	if err != nil || r.NextReview != today.AddDays(3) || r.M != 8 {
		t.Errorf("记住了 3 天后、掌握分 +8：%+v %v", r, err)
	}
	var answered bool
	_ = f.db.QueryRow("SELECT answered FROM kp_mastery WHERE owner_user_id = ? AND kp_id = ?", uid, kept.KPID).Scan(&answered)
	if answered {
		t.Error("背诵不算作答验证")
	}

	// 默写：按关键词覆盖判定。
	third := s.Items[2]
	r, err = f.pr.RecordRecite(ctx, uid, s.ID, practice.ReciteInput{KPID: third.KPID, Mode: "dictation", Text: third.OriginalText, Key: nextKey()})
	if err != nil || r.Result != "remembered" || len(r.Coverage) == 0 {
		t.Fatalf("默写全对：%+v %v", r, err)
	}
	for _, k := range r.Coverage {
		if !k.Hit {
			t.Errorf("默写原文应全部写到：%+v", r.Coverage)
		}
	}
	r, err = f.pr.RecordRecite(ctx, uid, s.ID, practice.ReciteInput{KPID: third.KPID, Mode: "dictation", Text: "完全不相关的内容", Key: nextKey()})
	if err != nil || r.Result != "forgot" {
		t.Errorf("默写没写到关键词：%+v %v", r, err)
	}
	if _, err := f.pr.RecordRecite(ctx, uid, s.ID, practice.ReciteInput{KPID: third.KPID, Mode: "cloze", Result: "maybe", Key: nextKey()}); kind(err) != apperr.BadRequest {
		t.Errorf("自评档位不对：%v", err)
	}

	sum, err := f.pr.FinishRecite(ctx, uid, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Forgot != 2 || sum.Remembered != 1 || sum.Tomorrow != 2 || sum.Later != 1 || sum.PreviousRemembered != nil {
		t.Errorf("背诵完成：%+v", sum)
	}
	retry, err := f.pr.CreateRecite(ctx, uid, sid, s.ID)
	if err != nil || len(retry.Items) != 2 || findItem(retry, forgot.Name).KPID == 0 || findItem(retry, kept.Name).KPID != 0 {
		t.Fatalf("再背没记住的：%+v %v", retry.Items, err)
	}
	if _, err := f.pr.RecordRecite(ctx, uid, retry.ID, practice.ReciteInput{KPID: forgot.KPID, Mode: "cloze", Result: "remembered", Key: nextKey()}); err != nil {
		t.Fatal(err)
	}
	sum2, _ := f.pr.FinishRecite(ctx, uid, retry.ID)
	if sum2.PreviousRemembered == nil || *sum2.PreviousRemembered != 1 {
		t.Errorf("与上一轮对比：%+v", sum2)
	}

	// 次日：没记住的出现在今日计划的背诵组；记住了的（3 天后）不出现（验收）。
	f.clock = f.clock.Add(24 * time.Hour)
	pl, err := f.plan.Today(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	recite := map[uint64]bool{}
	for _, it := range pl.Items {
		if it.Group == string(rules.GroupRecite) {
			recite[it.KPID] = true
		}
	}
	if !recite[third.KPID] || recite[kept.KPID] {
		t.Errorf("次日背诵组：%v（没记住的 %d 应在，记住了的 %d 不应在）", recite, third.KPID, kept.KPID)
	}
}

func TestOralReciteFlag(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	f.imported(t, uid, sid)
	s, err := f.pr.CreateRecite(ctx, uid, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	it := s.Items[0]
	// 开关关闭：口述入口不可用（404）。
	if _, err := f.pr.ReciteAudioUpload(ctx, uid, s.ID, "audio/m4a", 1000); kind(err) != apperr.NotFound {
		t.Errorf("开关关闭时录音：%v", err)
	}
	if _, err := f.pr.RecordRecite(ctx, uid, s.ID, practice.ReciteInput{KPID: it.KPID, Mode: "oral", AudioKey: "x", Key: nextKey()}); kind(err) != apperr.NotFound {
		t.Errorf("开关关闭时口述：%v", err)
	}
	// 只对这个用户打开。
	if _, err := f.db.Exec("INSERT INTO feature_flag_users (flag_key, user_id) VALUES ('oral_recite', ?)", uid); err != nil {
		t.Fatal(err)
	}
	f.flags.Invalidate()
	s, _ = f.pr.GetRecite(ctx, uid, s.ID)
	if !s.OralEnabled {
		t.Fatal("开关打开后应可口述")
	}
	up, err := f.pr.ReciteAudioUpload(ctx, uid, s.ID, "audio/m4a", 1000)
	if err != nil || !strings.HasSuffix(up.ObjectKey, ".m4a") || up.URL == "" {
		t.Fatalf("录音直传：%+v %v", up, err)
	}
	f.oss.Seed(up.ObjectKey, []byte("audio"))
	f.asr.Text = "我记得是" + it.OriginalText
	r, err := f.pr.RecordRecite(ctx, uid, s.ID, practice.ReciteInput{KPID: it.KPID, Mode: "oral", AudioKey: up.ObjectKey, Key: nextKey()})
	if err != nil || r.Result != "remembered" || r.Transcript == "" {
		t.Errorf("口述：%+v %v", r, err)
	}
	other, _ := f.user(t)
	theirs := "u/" + strconv.FormatUint(other, 10) + "/recite/1.m4a"
	f.oss.Seed(theirs, []byte("audio"))
	if _, err := f.pr.RecordRecite(ctx, uid, s.ID, practice.ReciteInput{KPID: it.KPID, Mode: "oral", AudioKey: theirs, Key: nextKey()}); kind(err) != apperr.NotFound {
		t.Errorf("不是自己的录音：%v", err)
	}
}

func TestReciteOwnership(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	a, sa := f.user(t)
	b, sb := f.user(t)
	f.imported(t, a, sa)
	s, err := f.pr.CreateRecite(ctx, a, sa, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pr.GetRecite(ctx, b, s.ID); kind(err) != apperr.NotFound {
		t.Errorf("别人的背诵：%v", err)
	}
	if _, err := f.pr.RecordRecite(ctx, b, s.ID, practice.ReciteInput{KPID: s.Items[0].KPID, Mode: "cloze", Result: "forgot", Key: nextKey()}); kind(err) != apperr.NotFound {
		t.Errorf("记别人的背诵：%v", err)
	}
	if _, err := f.pr.FinishRecite(ctx, b, s.ID); kind(err) != apperr.NotFound {
		t.Errorf("结束别人的背诵：%v", err)
	}
	if _, err := f.pr.CreateRecite(ctx, b, sa, 0); kind(err) != apperr.NotFound {
		t.Errorf("别人的专业课：%v", err)
	}
	if _, err := f.pr.CreateRecite(ctx, b, sb, s.ID); kind(err) != apperr.NotFound {
		t.Errorf("再背别人的一轮：%v", err)
	}
	// 练习会话接口不能拿来读背诵会话，背诵接口也不能读练习会话。
	ps, err := f.pr.Create(ctx, a, practice.CreateInput{SubjectID: sa, Kind: practice.KindTypeDrill, QType: "term"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pr.GetRecite(ctx, a, ps.ID); kind(err) != apperr.NotFound {
		t.Errorf("练习会话不是背诵：%v", err)
	}
}
