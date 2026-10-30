package bank_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/bank"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/gen"
	apihttp "peetraining-server/internal/http"
	"peetraining-server/internal/logx"
)

type bankTokens struct{ user uint64 }

func (b bankTokens) ParseAccess(string) (uint64, string, error) { return b.user, "test", nil }
func TestAnswerStatusHTTP(t *testing.T) {
	f := setup(t)
	uid, sid := f.user(t)
	var bid uint64
	if err := f.db.QueryRow("SELECT id FROM banks WHERE subject_id=?", sid).Scan(&bid); err != nil {
		t.Fatal(err)
	}
	types := []struct{ answer, origin, source string }{{"", "", "exam"}, {"答案", "ai_generated", "exam"}, {"答案", "user_confirmed", "exercise"}, {"答案", "", "exam"}, {"答案", "ai_generated", "ai_generated"}, {" \n\t", "ai_generated", "exam"}}
	for i := 0; i < 48; i++ {
		x := types[i%6]
		var origin any
		if x.origin != "" {
			origin = x.origin
		}
		if _, err := f.db.Exec(`INSERT INTO questions(owner_user_id,bank_id,qtype,stem,answer,answer_origin,source,content_hash) VALUES (?,?,'term',?,?,?,?,SHA2(?,256))`, uid, bid, fmt.Sprint(i), x.answer, origin, x.source, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	r, err := apihttp.NewRouter(apihttp.Deps{Bank: f.bank, Tokens: bankTokens{uid}, Logger: logx.New(io.Discard, slog.LevelInfo)})
	if err != nil {
		t.Fatal(err)
	}
	request := func(query string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/subjects/%d/questions?%s", sid, query), nil)
		req.Header.Set("Authorization", "Bearer test")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, tier := range []string{"missing", "ai", "material", "confirmed"} {
		t.Run(tier, func(t *testing.T) {
			cursor := "0"
			seen := map[int64]bool{}
			for {
				w := request("answer_status=" + tier + "&limit=3&cursor=" + cursor)
				if w.Code != 200 {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				var page gen.QuestionPage
				if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				for _, q := range page.Items {
					actual := string(*q.AnswerTier)
					if actual == "ai_question" {
						actual = "material"
					}
					if actual != tier || seen[q.Id] {
						t.Fatalf("filter=%s tier=%s duplicate=%v", tier, actual, seen[q.Id])
					}
					seen[q.Id] = true
				}
				if page.NextCursor == nil {
					break
				}
				cursor = *page.NextCursor
			}
			want := 8
			if tier == "missing" || tier == "material" {
				want = 16
			}
			if len(seen) != want {
				t.Fatalf("got %d want %d", len(seen), want)
			}
		})
	}
	for _, query := range []string{"answerStatus=missing", "answer=missing", "answer_state=ai", "answer_status=bogus", "answer_status=ai&answer_status=missing", "answer_status="} {
		if w := request(query); w.Code != 400 {
			t.Errorf("invalid %s accepted: %d", query, w.Code)
		}
	}
	if _, _, _, err := f.bank.Questions(context.Background(), uid, sid, bank.QuestionFilter{AnswerStatus: "bogus"}); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatal(err)
	}
}
func TestSplitPartitionsAllQuestionsAndKeepsMastery(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	uid, sid := f.user(t)
	section, err := f.bank.CreateNode(ctx, uid, sid, bank.NodeInput{Level: "section", Name: "板块"})
	if err != nil {
		t.Fatal(err)
	}
	src, err := f.bank.CreateNode(ctx, uid, sid, bank.NodeInput{Level: "point", Name: "待拆分", ParentID: &section.ID})
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.bank.CreateNode(ctx, uid, sid, bank.NodeInput{Level: "point", Name: "其他主知识点", ParentID: &section.ID})
	if err != nil {
		t.Fatal(err)
	}
	var bid uint64
	if err := f.db.QueryRow("SELECT id FROM banks WHERE subject_id=?", sid).Scan(&bid); err != nil {
		t.Fatal(err)
	}
	var ids []uint64
	for i := 0; i < 25; i++ {
		res, err := f.db.Exec(`INSERT INTO questions(owner_user_id,bank_id,qtype,stem,source,exam_year,content_hash) VALUES (?,?,'term',?,'exam',2024,SHA2(?,256))`, uid, bid, fmt.Sprint(i), fmt.Sprint(i))
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		ids = append(ids, uint64(id))
		if _, err := f.db.Exec("INSERT INTO question_kps(owner_user_id,question_id,kp_id,is_primary) VALUES (?,?,?,0),(?,?,?,1)", uid, id, src.ID, uid, id, other.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(`INSERT INTO kp_mastery(owner_user_id,kp_id,m,state,viewed,answered,last_self_assess,last_self_assess_at,correct_dates,next_review_on,interval_step,recite_next_review_on,recite_interval_step,decay_applied_on) VALUES (?,?,72,'consolidating',1,1,'vague','2026-10-01','["2026-10-01"]','2026-11-01',2,'2026-11-02',3,'2026-10-02')`, uid, src.ID); err != nil {
		t.Fatal(err)
	}
	original, err := dbq.New(f.db).GetKPMastery(ctx, dbq.GetKPMasteryParams{OwnerUserID: uid, KpID: src.ID})
	if err != nil {
		t.Fatal(err)
	}
	all, err := f.bank.SplitQuestions(ctx, uid, src.ID)
	if err != nil || len(all) != 25 {
		t.Fatalf("all questions: %d %v", len(all), err)
	}
	otherUser, otherSID := f.user(t)
	foreignSection, err := f.bank.CreateNode(ctx, otherUser, otherSID, bank.NodeInput{Level: "section", Name: "他人板块"})
	if err != nil {
		t.Fatal(err)
	}
	foreignKP, err := f.bank.CreateNode(ctx, otherUser, otherSID, bank.NodeInput{Level: "point", Name: "他人知识点", ParentID: &foreignSection.ID})
	if err != nil {
		t.Fatal(err)
	}
	qt, stem := "term", "他人的题"
	foreignQ, err := f.bank.CreateQuestion(ctx, otherUser, otherSID, bank.QuestionInput{QType: &qt, Stem: &stem, KPIDs: &[]uint64{foreignKP.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.bank.SplitQuestions(ctx, otherUser, src.ID); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatalf("split list ownership: %v", err)
	}
	if _, err := f.bank.Split(ctx, otherUser, src.ID, []bank.SplitPart{{Name: "a"}, {Name: "b"}}); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatalf("split ownership: %v", err)
	}
	invalid := [][]bank.SplitPart{
		{{Name: "a", QuestionIDs: append(append([]uint64{}, ids...), foreignQ.ID)}, {Name: "b"}},
		{{Name: "a"}, {Name: "b"}},
		{{Name: "a", QuestionIDs: ids[:12]}, {Name: "b", QuestionIDs: ids[13:]}},
		{{Name: "a", QuestionIDs: ids}, {Name: "b", QuestionIDs: ids[:1]}},
		{{Name: "a", QuestionIDs: append(append([]uint64{}, ids...), 99999999)}, {Name: "b"}},
		{{Name: "a", QuestionIDs: ids[:12]}, {Name: " ", QuestionIDs: ids[12:]}},
	}
	for i, parts := range invalid {
		if _, err := f.bank.Split(ctx, uid, src.ID, parts); !apperr.IsKind(err, apperr.BadRequest) {
			t.Fatalf("invalid %d: %v", i, err)
		}
		remaining, err := f.bank.SplitQuestions(ctx, uid, src.ID)
		if err != nil || len(remaining) != 25 {
			t.Fatal("failed split changed source")
		}
	}
	router, err := apihttp.NewRouter(apihttp.Deps{Bank: f.bank, Tokens: bankTokens{uid}, Logger: logx.New(io.Discard, slog.LevelInfo)})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"parts": []map[string]any{{"name": "a", "question_ids": ids[:12]}, {"name": "b", "question_ids": ids[12:]}}})
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/knowledge-points/%d/split", src.ID), strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != 200 {
		t.Fatalf("split HTTP %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Items []gen.KnowledgePointDetail `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 2 {
		t.Fatalf("split response: %s", recorder.Body.String())
	}
	var parts []bank.KPDetail
	for _, item := range response.Items {
		part, err := f.bank.KnowledgePoint(ctx, uid, uint64(item.Id))
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, part)
	}
	for i, p := range parts {
		want := 12
		if i == 1 {
			want = 13
		}
		qs, _, _, err := f.bank.Questions(ctx, uid, sid, bank.QuestionFilter{KPID: &p.ID, Limit: 100})
		if err != nil || len(qs) != want || p.ExamCount != want {
			t.Fatalf("partition %d: %d exams %d err %v", i, len(qs), p.ExamCount, err)
		}
		m, err := dbq.New(f.db).GetKPMastery(ctx, dbq.GetKPMasteryParams{OwnerUserID: uid, KpID: p.ID})
		if err != nil {
			t.Fatal(err)
		}
		m.KpID = original.KpID
		m.UpdatedAt = original.UpdatedAt
		got, _ := json.Marshal(m)
		wantMastery, _ := json.Marshal(original)
		if string(got) != string(wantMastery) {
			t.Fatalf("mastery: %s != %s", got, wantMastery)
		}
	}
	if _, err := f.bank.KnowledgePoint(ctx, uid, src.ID); !apperr.IsKind(err, apperr.NotFound) {
		t.Fatal("source must be removed")
	}
	var links, primary int
	if err := f.db.QueryRow("SELECT COUNT(*),SUM(is_primary) FROM question_kps WHERE owner_user_id=?", uid).Scan(&links, &primary); err != nil || links != 50 || primary != 25 {
		t.Fatalf("links %d primary %d err %v", links, primary, err)
	}
}
