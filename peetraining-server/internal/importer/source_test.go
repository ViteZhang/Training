package importer

import (
	"testing"

	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
)

func TestNormalizeQuestionSources(t *testing.T) {
	got, err := normalizeQuestionSources(dbq.ImportJobsModeQuestion, []uint64{10, 20}, []FileQuestionSource{
		{MaterialID: 10, Source: "exam"}, {MaterialID: 20, Source: "exercise"},
	})
	if err != nil || got[10] != "exam" || got[20] != "exercise" {
		t.Fatalf("per-file sources: %v %+v", err, got)
	}

	legacy, err := normalizeQuestionSources(dbq.ImportJobsModeQuestion, []uint64{10}, nil)
	if err != nil || legacy[10] != "unconfirmed" {
		t.Fatalf("missing legacy source must remain unconfirmed: %v %+v", err, legacy)
	}

	for name, choices := range map[string][]FileQuestionSource{
		"missing file":          {{MaterialID: 10, Source: "exam"}},
		"unknown file":          {{MaterialID: 10, Source: "exam"}, {MaterialID: 30, Source: "exercise"}},
		"invalid source":        {{MaterialID: 10, Source: "official"}},
		"conflicting duplicate": {{MaterialID: 10, Source: "exam"}, {MaterialID: 10, Source: "exercise"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := normalizeQuestionSources(dbq.ImportJobsModeQuestion, []uint64{10, 20}, choices); !apperr.IsKind(err, apperr.BadRequest) {
				t.Fatalf("expected bad request, got %v", err)
			}
		})
	}

	if _, err := normalizeQuestionSources(dbq.ImportJobsModeReference, []uint64{10}, []FileQuestionSource{}); !apperr.IsKind(err, apperr.BadRequest) {
		t.Fatalf("non-question mode must reject source choices, got %v", err)
	}
}

func TestValidateQuestionDoesNotInferSourceFromYear(t *testing.T) {
	year := 2024
	q := QuestionDraft{QType: "term", Stem: "意境", ExamYear: &year}
	if err := validateQuestion(&q, QuestionDraft{}); err != nil {
		t.Fatal(err)
	}
	if q.Source != "unconfirmed" {
		t.Fatalf("year must not infer source: %q", q.Source)
	}

	q.Source = ""
	if err := validateQuestion(&q, QuestionDraft{Source: "exercise"}); err != nil {
		t.Fatal(err)
	}
	if q.Source != "exercise" {
		t.Fatalf("missing patch source must preserve prior choice: %q", q.Source)
	}
}
