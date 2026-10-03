package importer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"peetraining-server/internal/ai"
	"peetraining-server/internal/apperr"
	"peetraining-server/internal/dbq"
	"peetraining-server/internal/jobs"
	"peetraining-server/internal/material"
	"peetraining-server/internal/quota"
	"peetraining-server/internal/store"
)

// Enqueuer 把任务放进队列（*asynq.Client 实现）；测试里换成同步执行。
type Enqueuer interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}

// Service 是导入流水线。
type Service struct {
	db       *sql.DB
	q        *dbq.Queries
	material *material.Service
	quota    *quota.Service
	ai       *ai.Engine
	queue    Enqueuer
	now      func() time.Time
	// AfterConfirm 在确认入库后调用（T16 生成今日计划，返回是否已生成）。
	AfterConfirm func(ctx context.Context, userID, subjectID uint64) bool
	// OnConfirmedTx 在确认入库的同一事务里调用（T26：被邀请人第一次导入资料后发邀请奖励）。
	OnConfirmedTx func(ctx context.Context, q *dbq.Queries, userID uint64) error
}

// Deps 是创建服务的依赖。
type Deps struct {
	DB       *sql.DB
	Material *material.Service
	Quota    *quota.Service
	AI       *ai.Engine
	Queue    Enqueuer
	Now      func() time.Time
}

func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{db: d.DB, q: dbq.New(d.DB), material: d.Material, quota: d.Quota, ai: d.AI, queue: d.Queue, now: now}
}

// Job 是导入任务与各文件进度、条目统计（1.6、1.6b）。
type Job struct {
	dbq.GetImportJobRow
	Materials []dbq.ListImportJobMaterialsRow
	Counts    dbq.CountImportItemsRow
	// DetectedEssay 表示这批资料里作文类过半（1.7 显示，可在专业课设置里改）。
	DetectedEssay bool
}

// Get 返回任务；不是自己的返回 404。
func (s *Service) Get(ctx context.Context, userID, jobID uint64) (Job, error) {
	j, err := s.q.GetImportJob(ctx, dbq.GetImportJobParams{ID: jobID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, apperr.NotFoundErr()
	}
	if err != nil {
		return Job{}, err
	}
	ms, err := s.q.ListImportJobMaterials(ctx, dbq.ListImportJobMaterialsParams{JobID: jobID, OwnerUserID: userID})
	if err != nil {
		return Job{}, err
	}
	c, err := s.q.CountImportItems(ctx, dbq.CountImportItemsParams{JobID: jobID, OwnerUserID: userID})
	if err != nil {
		return Job{}, err
	}
	essay, classified := 0, 0
	for _, m := range ms {
		if m.Category.Valid {
			classified++
			if m.Category.MaterialsCategory == dbq.MaterialsCategoryEssay {
				essay++
			}
		}
	}
	return Job{GetImportJobRow: j, Materials: ms, Counts: c, DetectedEssay: classified > 0 && essay*2 > classified}, nil
}

// List 返回我的导入任务（activeOnly：解析中或待确认，首页 2.1b 用）。
func (s *Service) List(ctx context.Context, userID uint64, activeOnly bool) ([]Job, error) {
	active := 0
	if activeOnly {
		active = 1
	}
	ids, err := s.q.ListImportJobs(ctx, dbq.ListImportJobsParams{OwnerUserID: userID, ActiveOnly: active})
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(ids))
	for _, id := range ids {
		j, err := s.Get(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}

// knownPages 是建任务时能确定的计费页数：PDF 用客户端页数，图片 1，粘贴已分好页；Word、Excel 要取完文本才知道，先按 0，取文本后补预占。
func knownPages(m material.Material) int {
	switch m.Format {
	case dbq.MaterialsFormatImage:
		return 1
	case dbq.MaterialsFormatPdf, dbq.MaterialsFormatText:
		return int(m.PageCount)
	}
	return 0
}

// CreateJob 开始解析（1.5 → 1.6）：核对资料都已上传且属于这门课，按页数预占解析额度（不足在开始前返回 402），提交后逐个文件入队。
func (s *Service) CreateJob(ctx context.Context, userID, subjectID uint64, mode string, materialIDs []uint64) (Job, error) {
	m := dbq.ImportJobsMode(mode)
	if !m.Valid() {
		return Job{}, apperr.New(apperr.BadRequest, "导入方式不正确")
	}
	var bankID uint64
	mats := make([]material.Material, 0, len(materialIDs))
	seen := map[uint64]bool{}
	for _, id := range materialIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		mt, err := s.material.Get(ctx, userID, id)
		if err != nil {
			return Job{}, err
		}
		if uint64(mt.SubjectID.Int64) != subjectID {
			return Job{}, apperr.NotFoundErr()
		}
		if mt.Status == dbq.MaterialsStatusUploading {
			return Job{}, apperr.New(apperr.BadRequest, fmt.Sprintf("「%s」还没有上传完成", mt.FileName)).With("reason", "not_uploaded").With("material_id", id)
		}
		bankID = mt.BankID
		mats = append(mats, mt)
	}
	if len(mats) == 0 {
		return Job{}, apperr.New(apperr.BadRequest, "请选择要导入的资料")
	}
	var jobID uint64
	err := store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		id, err := q.CreateImportJob(ctx, dbq.CreateImportJobParams{OwnerUserID: userID, BankID: bankID, Mode: m})
		if err != nil {
			return err
		}
		jobID = uint64(id)
		total := 0
		for _, mt := range mats {
			if err := q.AddImportJobMaterial(ctx, dbq.AddImportJobMaterialParams{JobID: jobID, MaterialID: mt.ID, OwnerUserID: userID}); err != nil {
				return err
			}
			pages := knownPages(mt)
			t, err := s.quota.Reserve(ctx, q, quota.Charge{UserID: userID, Type: quota.ParsePages, Amount: pages,
				Ref: quota.Ref{Type: "material", ID: mt.ID}, Key: reserveKey(jobID, mt.ID)})
			if err != nil {
				return err
			}
			if err := q.SetImportJobMaterialQuota(ctx, dbq.SetImportJobMaterialQuotaParams{
				ReservedPages: uint32(pages), QuotaPeriod: sql.NullString{String: t.Period, Valid: true},
				JobID: jobID, MaterialID: mt.ID, OwnerUserID: userID,
			}); err != nil {
				return err
			}
			total += pages
		}
		return q.AddImportJobPages(ctx, dbq.AddImportJobPagesParams{Reserved: uint32(total), ID: jobID, OwnerUserID: userID})
	})
	if err != nil {
		return Job{}, err
	}
	// 入队在事务提交之后（CLAUDE.md 必须遵守第 12 条）。入队失败不回滚任务，用户可在 1.6b 重试。
	for _, mt := range mats {
		if err := s.enqueueFile(ctx, userID, jobID, mt.ID, 0); err != nil {
			return Job{}, err
		}
	}
	return s.Get(ctx, userID, jobID)
}

func reserveKey(jobID, materialID uint64) string {
	return fmt.Sprintf("import:%d:%d:reserve", jobID, materialID)
}

func (s *Service) enqueueFile(ctx context.Context, userID, jobID, materialID uint64, attempt int) error {
	t, err := jobs.NewImportFileTask(userID, jobID, materialID, attempt)
	if err != nil {
		return err
	}
	return ignoreDup(s.queue.EnqueueContext(ctx, t))
}

func (s *Service) enqueueFinish(ctx context.Context, userID, jobID uint64) error {
	t, err := jobs.NewImportFinishTask(userID, jobID)
	if err != nil {
		return err
	}
	return ignoreDup(s.queue.EnqueueContext(ctx, t))
}

func ignoreDup(_ *asynq.TaskInfo, err error) error {
	if errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict) {
		return nil
	}
	return err
}

func (s *Service) jobMaterial(ctx context.Context, userID, jobID, materialID uint64) (dbq.ImportJobMaterial, error) {
	jm, err := s.q.GetImportJobMaterial(ctx, dbq.GetImportJobMaterialParams{JobID: jobID, MaterialID: materialID, OwnerUserID: userID})
	if errors.Is(err, sql.ErrNoRows) {
		return jm, apperr.NotFoundErr()
	}
	return jm, err
}

// Retry 重新解析失败的文件（1.6b，如重新拍照后）：重新预占额度，从取文本开始。
func (s *Service) Retry(ctx context.Context, userID, jobID, materialID uint64) (Job, error) {
	jm, err := s.jobMaterial(ctx, userID, jobID, materialID)
	if err != nil {
		return Job{}, err
	}
	if jm.Status != dbq.ImportJobMaterialsStatusFailed && jm.Status != dbq.ImportJobMaterialsStatusPartial {
		return Job{}, apperr.New(apperr.Conflict, "这个文件正在解析或已完成，不需要重试")
	}
	mt, err := s.material.Get(ctx, userID, materialID)
	if err != nil {
		return Job{}, err
	}
	if mt.Status == dbq.MaterialsStatusRejected {
		return Job{}, apperr.New(apperr.Conflict, "这份资料未通过内容安全审核，不能重新解析")
	}
	attempt := int(jm.Attempts) + 1
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		pages := max(knownPages(mt), int(mt.BilledPages))
		t, err := s.quota.Reserve(ctx, q, quota.Charge{UserID: userID, Type: quota.ParsePages, Amount: pages,
			Ref: quota.Ref{Type: "material", ID: materialID}, Key: fmt.Sprintf("%s:%d", reserveKey(jobID, materialID), attempt)})
		if err != nil {
			return err
		}
		if err := q.SetImportJobMaterialQuota(ctx, dbq.SetImportJobMaterialQuotaParams{
			ReservedPages: uint32(pages), QuotaPeriod: sql.NullString{String: t.Period, Valid: true},
			JobID: jobID, MaterialID: materialID, OwnerUserID: userID,
		}); err != nil {
			return err
		}
		if err := q.UpdateImportJobMaterial(ctx, dbq.UpdateImportJobMaterialParams{
			Step: dbq.ImportJobMaterialsStepQueued, Status: dbq.ImportJobMaterialsStatusPending, Attempts: uint8(min(attempt, 255)),
			JobID: jobID, MaterialID: materialID, OwnerUserID: userID,
		}); err != nil {
			return err
		}
		return q.UpdateImportJobStatus(ctx, dbq.UpdateImportJobStatusParams{Status: dbq.ImportJobsStatusRunning, ID: jobID, OwnerUserID: userID})
	})
	if err != nil {
		return Job{}, err
	}
	if err := s.enqueueFile(ctx, userID, jobID, materialID, attempt); err != nil {
		return Job{}, err
	}
	return s.Get(ctx, userID, jobID)
}

// Remove 从任务里移除文件（1.6b「移除」）：还没确认的条目删掉，没结算的预占退回。
func (s *Service) Remove(ctx context.Context, userID, jobID, materialID uint64) (Job, error) {
	jm, err := s.jobMaterial(ctx, userID, jobID, materialID)
	if err != nil {
		return Job{}, err
	}
	err = store.WithTx(ctx, s.db, func(q *dbq.Queries) error {
		if err := s.settle(ctx, q, jm, 0); err != nil {
			return err
		}
		mid := sql.NullInt64{Int64: int64(materialID), Valid: true}
		if err := q.DeleteImportItemsOfMaterial(ctx, dbq.DeleteImportItemsOfMaterialParams{JobID: jobID, MaterialID: mid, OwnerUserID: userID}); err != nil {
			return err
		}
		if err := q.DeleteImportAnswersOfMaterial(ctx, dbq.DeleteImportAnswersOfMaterialParams{JobID: jobID, MaterialID: materialID, OwnerUserID: userID}); err != nil {
			return err
		}
		_, err := q.DeleteImportJobMaterial(ctx, dbq.DeleteImportJobMaterialParams{JobID: jobID, MaterialID: materialID, OwnerUserID: userID})
		return err
	})
	if err != nil {
		return Job{}, err
	}
	if err := s.enqueueFinish(ctx, userID, jobID); err != nil {
		return Job{}, err
	}
	return s.Get(ctx, userID, jobID)
}

// settle 结算一个文件的预占：成功按计费页数扣（actual），失败或移除传 0 全部退回。已结算的不重复处理。
func (s *Service) settle(ctx context.Context, q *dbq.Queries, jm dbq.ImportJobMaterial, actual int) error {
	if jm.Settled {
		return nil
	}
	t := quota.Ticket{UserID: jm.OwnerUserID, Type: quota.ParsePages, Period: jm.QuotaPeriod.String, Amount: int(jm.ReservedPages),
		Key: fmt.Sprintf("%s:%d", reserveKey(jm.JobID, jm.MaterialID), jm.Attempts)}
	if err := s.quota.Settle(ctx, q, t, actual, quota.Ref{Type: "material", ID: jm.MaterialID}); err != nil {
		return err
	}
	if err := q.AddImportJobPages(ctx, dbq.AddImportJobPagesParams{Billed: uint32(max(actual, 0)), ID: jm.JobID, OwnerUserID: jm.OwnerUserID}); err != nil {
		return err
	}
	return q.SetImportJobMaterialQuota(ctx, dbq.SetImportJobMaterialQuotaParams{
		ReservedPages: jm.ReservedPages, QuotaPeriod: jm.QuotaPeriod, Settled: true,
		JobID: jm.JobID, MaterialID: jm.MaterialID, OwnerUserID: jm.OwnerUserID,
	})
}
