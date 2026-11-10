package official

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"peetraining-server/internal/dbq"
)

// T56 题目要对上知识点：题目按名称关联知识点，发布时对不上的关联会丢掉。
// 核对区标出「知识点不存在」的题和「已有同名知识点」的知识点；发布前检查也拦下关联了不存在知识点的题。

// kpNames 是项目里能被题目关联到的知识点名称：已发布的、草稿里的（没被退回的）、各份资料解析出来还没驳回的。
type kpNames struct {
	published map[string]bool
	drafts    map[string]bool
	imported  []dbq.ListProjectImportKPsRow
}

func (n kpNames) has(name string) bool {
	if n.published[name] || n.drafts[name] {
		return true
	}
	for _, r := range n.imported {
		var k KPPayload
		if json.Unmarshal(r.Payload, &k) == nil && k.Name == name {
			return true
		}
	}
	return false
}

func (s *Service) loadKPNames(ctx context.Context, projectID uint64) (kpNames, error) {
	out := kpNames{published: map[string]bool{}, drafts: map[string]bool{}}
	p, err := s.Project(ctx, projectID)
	if err != nil {
		return out, err
	}
	kps, err := s.q.ListOfficialKPs(ctx, p.BankID)
	if err != nil {
		return out, err
	}
	for _, k := range kps {
		if k.Level == dbq.KnowledgePointsLevelPoint {
			out.published[k.Name] = true
		}
	}
	drafts, err := s.Drafts(ctx, projectID, "", 0)
	if err != nil {
		return out, err
	}
	for _, d := range drafts {
		if d.EntityType != string(dbq.OfficialDraftsEntityTypeKnowledgePoint) || d.ChangeType == string(dbq.OfficialDraftsChangeTypeOffline) ||
			d.Status == string(dbq.OfficialDraftsStatusRejected) {
			continue
		}
		var k KPPayload
		if json.Unmarshal(d.Payload, &k) == nil && k.Name != "" {
			out.drafts[k.Name] = true
		}
	}
	out.imported, err = s.q.ListProjectImportKPs(ctx, projectID)
	return out, err
}

// markKPs 给一份资料里还没核对的条目标知识点情况：题目关联了不存在的知识点（MissingKPs）；
// 知识点在题库里已有同名的（ExistingKP，接受后按修订处理）；与别的资料或草稿里还没发布的知识点重名（Duplicate）。
func (s *Service) markKPs(ctx context.Context, f dbq.OfficialImportFile, items []ImportItem) error {
	pending := false
	for _, it := range items {
		pending = pending || it.Status == string(dbq.OfficialImportItemsStatusPending)
	}
	if !pending {
		return nil
	}
	names, err := s.loadKPNames(ctx, f.ProjectID)
	if err != nil {
		return err
	}
	for i := range items {
		it := &items[i]
		if it.Status != string(dbq.OfficialImportItemsStatusPending) {
			continue
		}
		switch it.ItemType {
		case string(dbq.OfficialImportItemsItemTypeQuestion):
			var p QuestionPayload
			if json.Unmarshal(it.Payload, &p) != nil {
				continue
			}
			for _, n := range p.KPNames {
				if n = strings.TrimSpace(n); n != "" && !names.has(n) && !slices.Contains(it.MissingKPs, n) {
					it.MissingKPs = append(it.MissingKPs, n)
				}
			}
		case string(dbq.OfficialImportItemsItemTypeKnowledgePoint):
			var k KPPayload
			if json.Unmarshal(it.Payload, &k) != nil {
				continue
			}
			it.ExistingKP = names.published[k.Name]
			if it.ExistingKP || it.Duplicate != "" {
				continue
			}
			// 已发布的同名知识点接受后是修订；还没发布的同名（别的资料里排在前面的、草稿里的）再接受会重复新建，标为重复。
			for _, r := range names.imported {
				if r.ID == it.ID || r.FileID > f.ID || (r.FileID == f.ID && int(r.Seq) > it.Seq) {
					continue
				}
				var o KPPayload
				if json.Unmarshal(r.Payload, &o) == nil && o.Name == k.Name {
					if r.FileID == f.ID {
						it.Duplicate = fmt.Sprintf("与本资料第 %d 条同名", r.Seq)
					} else {
						it.Duplicate = fmt.Sprintf("与《%s》第 %d 条同名", r.FileName, r.Seq)
					}
					break
				}
			}
			if it.Duplicate == "" && names.drafts[k.Name] {
				it.Duplicate = "与草稿里还没发布的知识点同名"
			}
			if it.Duplicate != "" {
				it.Confidence = string(dbq.OfficialImportItemsConfidenceLow)
			}
		}
	}
	return nil
}

// questionKPCheck 是发布前检查「题目关联的知识点都存在」：这次要发布的题目（新增、修订）关联的知识点，
// 要么已发布，要么在这次发布的知识点变更里。返回对不上的「题目：知识点」说明。
func questionKPCheck(drafts []dbq.OfficialDraft, published map[string]bool) []string {
	names := map[string]bool{}
	for k := range published {
		names[k] = true
	}
	for _, d := range drafts {
		if d.EntityType == dbq.OfficialDraftsEntityTypeKnowledgePoint && d.ChangeType != dbq.OfficialDraftsChangeTypeOffline {
			var k KPPayload
			if json.Unmarshal(d.Payload, &k) == nil {
				names[k.Name] = true
			}
		}
	}
	var out []string
	for _, d := range drafts {
		if d.EntityType != dbq.OfficialDraftsEntityTypeQuestion || d.ChangeType == dbq.OfficialDraftsChangeTypeOffline {
			continue
		}
		var p QuestionPayload
		if json.Unmarshal(d.Payload, &p) != nil {
			continue
		}
		for _, n := range p.KPNames {
			if n = strings.TrimSpace(n); n != "" && !names[n] {
				out = append(out, fmt.Sprintf("「%s」的知识点「%s」", cutRunes(p.Stem, 15), n))
			}
		}
	}
	return out
}

// pointNames 是官方题库现有「知识点」一级的名称。
func (c content) pointNames() map[string]bool {
	out := map[string]bool{}
	for _, k := range c.kps {
		if k.Level == dbq.KnowledgePointsLevelPoint {
			out[k.Name] = true
		}
	}
	return out
}
