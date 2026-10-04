package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

type PlanStatus string

const (
	PlanDraft     PlanStatus = "draft"
	PlanApproved  PlanStatus = "approved"
	PlanActive    PlanStatus = "active"
	PlanCompleted PlanStatus = "completed"
	PlanCancelled PlanStatus = "cancelled"
)

type PrepPlan struct {
	ID             string     `json:"id"`
	SpecimenID     string     `json:"specimen_id"`
	Objective      string     `json:"objective"`
	Method         string     `json:"method"`
	Adhesive       string     `json:"adhesive"`
	TargetExposure int        `json:"target_exposure"`
	Status         PlanStatus `json:"status"`
	ApprovedBy     string     `json:"approved_by,omitempty"`
	ApprovedAt     *time.Time `json:"approved_at,omitempty"`
	// ReevaluationRequired is set when specimen registration data the draft
	// plan relies on (matrix kind or taxon hint) is corrected. The draft
	// cannot be approved until the responsible lead re-confirms it.
	ReevaluationRequired bool       `json:"reevaluation_required"`
	ReevaluatedBy        string     `json:"reevaluated_by,omitempty"`
	ReevaluatedAt        *time.Time `json:"reevaluated_at,omitempty"`
	ReevaluationNote     string     `json:"reevaluation_note,omitempty"`
	Version              int        `json:"version"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func NewPrepPlan(
	id string,
	specimen Specimen,
	objective string,
	method string,
	adhesive string,
	targetExposure int,
	now time.Time,
) (PrepPlan, error) {
	if id == "" {
		return PrepPlan{}, contracts.Internal("missing_generated_id", "方案标识未生成", nil)
	}
	if !specimen.CanCreatePlan() {
		return PrepPlan{}, contracts.Conflict("specimen_not_plannable", "当前标本状态不能创建修复方案")
	}
	goal, err := requiredText("修复目标", objective, 200)
	if err != nil {
		return PrepPlan{}, err
	}
	approach, err := requiredText("修复方法", method, 160)
	if err != nil {
		return PrepPlan{}, err
	}
	adhesiveValue, err := optionalText("黏结材料", adhesive, 120)
	if err != nil {
		return PrepPlan{}, err
	}
	if err := validatePercent(targetExposure); err != nil {
		return PrepPlan{}, contracts.Invalid("invalid_target_exposure", "目标暴露比例必须位于 0 到 100 之间")
	}
	if targetExposure == 0 {
		return PrepPlan{}, contracts.Invalid("invalid_target_exposure", "目标暴露比例必须大于零")
	}
	return PrepPlan{
		ID:             id,
		SpecimenID:     specimen.ID,
		Objective:      goal,
		Method:         approach,
		Adhesive:       adhesiveValue,
		TargetExposure: targetExposure,
		Status:         PlanDraft,
		Version:        1,
		CreatedAt:      now.UTC(),
		UpdatedAt:      now.UTC(),
	}, nil
}

func (p PrepPlan) IsOpen() bool {
	return p.Status == PlanDraft || p.Status == PlanApproved || p.Status == PlanActive
}

func (p PrepPlan) IsCompleted() bool {
	return p.Status == PlanCompleted
}

func (p *PrepPlan) Approve(approvedBy string, now time.Time) error {
	if p.Status != PlanDraft {
		return contracts.Conflict("plan_not_approvable", "只有草稿方案可以审批")
	}
	if p.ReevaluationRequired {
		return contracts.Conflict(
			"plan_reevaluation_pending",
			"标本资料已更正，草稿方案必须先重新评估并确认才能审批",
		)
	}
	approver, err := requiredText("审批人", approvedBy, 80)
	if err != nil {
		return err
	}
	stamp := now.UTC()
	p.Status = PlanApproved
	p.ApprovedBy = approver
	p.ApprovedAt = &stamp
	p.Version++
	p.UpdatedAt = stamp
	return nil
}

// MarkReevaluationRequired flags a draft plan whose underlying specimen data
// changed in a way that affects its method or target. It is idempotent: a
// plan already awaiting reevaluation keeps the flag and its version.
func (p *PrepPlan) MarkReevaluationRequired(now time.Time) error {
	if p.Status != PlanDraft {
		return contracts.Conflict(
			"plan_not_reevaluable",
			"只有草稿方案可以因资料更正进入重新评估",
		)
	}
	if p.ReevaluationRequired {
		return nil
	}
	p.ReevaluationRequired = true
	p.ReevaluatedBy = ""
	p.ReevaluatedAt = nil
	p.ReevaluationNote = ""
	p.Version++
	p.UpdatedAt = now.UTC()
	return nil
}

// ResolveReevaluation records the lead's decision that the draft method and
// target still hold after the specimen data correction.
func (p *PrepPlan) ResolveReevaluation(reviewedBy, note string, now time.Time) error {
	if p.Status != PlanDraft {
		return contracts.Conflict(
			"plan_not_reevaluable",
			"只有草稿方案可以处理重新评估",
		)
	}
	if !p.ReevaluationRequired {
		return contracts.Conflict(
			"plan_reevaluation_not_required",
			"该方案当前没有待处理的重新评估",
		)
	}
	reviewer, err := requiredText("重新评估确认人", reviewedBy, 80)
	if err != nil {
		return err
	}
	noteValue, err := optionalText("重新评估说明", note, 300)
	if err != nil {
		return err
	}
	stamp := now.UTC()
	p.ReevaluationRequired = false
	p.ReevaluatedBy = reviewer
	p.ReevaluatedAt = &stamp
	p.ReevaluationNote = noteValue
	p.Version++
	p.UpdatedAt = stamp
	return nil
}

func (p *PrepPlan) Begin(now time.Time) error {
	if p.Status == PlanActive {
		return nil
	}
	if p.Status != PlanApproved {
		return contracts.Conflict("plan_not_active", "只有已审批方案可以开始制备")
	}
	p.Status = PlanActive
	p.Version++
	p.UpdatedAt = now.UTC()
	return nil
}

func (p *PrepPlan) Complete(now time.Time) error {
	if p.Status == PlanCompleted {
		return nil
	}
	if p.Status != PlanActive {
		return contracts.Conflict("plan_not_completable", "只有进行中的方案可以完成")
	}
	p.Status = PlanCompleted
	p.Version++
	p.UpdatedAt = now.UTC()
	return nil
}

func (p *PrepPlan) Cancel(now time.Time) error {
	if p.Status != PlanDraft {
		return contracts.Conflict("plan_not_cancellable", "只有草稿方案可以取消")
	}
	p.Status = PlanCancelled
	p.Version++
	p.UpdatedAt = now.UTC()
	return nil
}
