package practice

import (
	"database/sql"
	"testing"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/dbq"
)

func TestSubResultsGroupBySubquestion(t *testing.T) {
	subs := []dbq.QuestionSubquestion{
		{ID: 11, Ordinal: 1, Label: "(1)", FullScore: sql.NullString{String: "4", Valid: true}},
		{ID: 12, Ordinal: 2, Label: "(2)", FullScore: sql.NullString{String: "5", Valid: true}},
	}
	snap := rubricSnap{Points: []ai.GradePoint{
		{Seq: 1, Score: 2, SubquestionID: 11},
		{Seq: 2, Score: 2, SubquestionID: 11},
		{Seq: 3, Score: 5, SubquestionID: 12},
	}}
	out := ai.GradeOut{Points: []ai.GradeResult{
		{Seq: 1, Verdict: "hit", Score: 2},
		{Seq: 2, Verdict: "miss", Score: 0, Reason: "没有结合材料"},
		{Seq: 3, Verdict: "partial", Score: 3},
	}}
	got := subResults(subs, snap, out)
	if len(got) != 2 || got[0].SubquestionID != 11 || got[0].Score != 2 || got[0].FullScore != 4 {
		t.Fatalf("第一问汇总不对：%+v", got)
	}
	if got[1].SubquestionID != 12 || got[1].Score != 3 || got[1].FullScore != 5 || got[1].Feedback != "" {
		t.Fatalf("第二问汇总不对：%+v", got)
	}
}
