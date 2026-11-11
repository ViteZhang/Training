package practice

import (
	"context"
	"errors"

	"peetraining-server/internal/dbq"
	"peetraining-server/internal/rules"
)

// 今日训练的下一段：practice 刷题、recite 背诵、done 今天没有可做的内容了。
const (
	StepPractice = "practice"
	StepRecite   = "recite"
	StepDone     = "done"
)

// TodayStep 是今日训练的下一段。Remaining 是今天计划里还没做的条目数（含这一段）。
type TodayStep struct {
	Step      string
	SessionID uint64
	SubjectID uint64
	Remaining int
}

// ContinueToday 是首页与训练页的「开始训练 / 继续训练」。今日训练 = 今天计划的全部条目（题目与背诵，跨所有专业课），
// 这里按「这门课的题 → 这门课的背诵 → 下一门课」找到下一段还没做的内容并建好会话，用户不用自己切课、找背诵入口。
// prefer 是优先开始的专业课（训练页当前选中的课），0 表示按计划顺序。
func (s *Service) ContinueToday(ctx context.Context, userID, prefer uint64, aiFill bool) (TodayStep, error) {
	pl, err := s.plan.Today(ctx, userID)
	if err != nil {
		return TodayStep{}, err
	}
	out := TodayStep{Step: StepDone, Remaining: len(pl.Items) - len(pl.Done)}
	type todo struct {
		questions bool
		recite    map[uint64]bool
	}
	left := map[uint64]*todo{}
	var order []uint64
	for i, it := range pl.Items {
		if pl.Done[i] {
			continue
		}
		t := left[it.SubjectID]
		if t == nil {
			t = &todo{recite: map[uint64]bool{}}
			left[it.SubjectID] = t
			if it.SubjectID == prefer {
				order = append([]uint64{prefer}, order...)
			} else {
				order = append(order, it.SubjectID)
			}
		}
		if it.Group == string(rules.GroupRecite) {
			t.recite[it.KPID] = true
		} else if it.QuestionID != 0 {
			t.questions = true
		}
	}
	for _, sid := range order {
		t := left[sid]
		if t.questions {
			sess, err := s.Create(ctx, userID, CreateInput{SubjectID: sid, Kind: KindDaily, AIFill: aiFill})
			if err == nil {
				out.Step, out.SessionID, out.SubjectID = StepPractice, sess.ID, sid
				return out, nil
			}
			// 计划里的题已经不在题库里（被删除或下线）时跳过，接着看背诵和下一门课。
			if !errors.Is(err, errDailyDone) {
				return TodayStep{}, err
			}
		}
		if len(t.recite) == 0 {
			continue
		}
		ok, err := s.recitable(ctx, userID, sid, t.recite)
		if err != nil {
			return TodayStep{}, err
		}
		if !ok {
			continue
		}
		rs, err := s.CreateRecite(ctx, userID, sid, 0)
		if err != nil {
			return TodayStep{}, err
		}
		out.Step, out.SessionID, out.SubjectID = StepRecite, rs.ID, sid
		return out, nil
	}
	return out, nil
}

// recitable 判断这些知识点里是否还有能背的（有原文表述）。没有原文的背不了，不能让「继续训练」反复进一轮空的背诵。
func (s *Service) recitable(ctx context.Context, userID, subjectID uint64, kps map[uint64]bool) (bool, error) {
	b, err := s.subject(ctx, userID, subjectID)
	if err != nil {
		return false, err
	}
	cands, err := s.q.ListReciteCandidates(ctx, dbq.ListReciteCandidatesParams{BankID: b.BankID, OwnerUserID: owner(userID)})
	if err != nil {
		return false, err
	}
	for _, c := range cands {
		if kps[c.ID] {
			return true, nil
		}
	}
	return false, nil
}
