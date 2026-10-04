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
	ID                   string     `json:"id"`
	SpecimenID           string     `json:"specimen_id"`
	Objective            string     `json:"objective"`
	Method               string     `json:"method"`
	Adhesive             string     `json:"adhesive"`
	TargetExposure       int        `json:"target_exposure"`
	Status               PlanStatus `json:"status"`
	ReevaluationRequired bool       `json:"reevaluation_required"`
	ApprovedBy           string     `json:"approved_by,omitempty"`
	ApprovedAt           *time.Time `json:"approved_at,omitempty"`
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

// MarkReevaluationRequired flags a draft plan whose specimen data changed
// in a way that affects its objective or method. The plan must not be
// approved until the responsible person confirms the re-evaluation.
func (p *PrepPlan) MarkReevaluationRequired(now time.Time) error {
	if p.Status != PlanDraft {
		return contracts.Conflict("plan_not_reevaluable", "只有草稿方案可以标记重新评估")
	}
	if p.ReevaluationRequired {
		return nil
	}
	p.ReevaluationRequired = true
	p.Version++
	p.UpdatedAt = now.UTC()
	return nil
}

// ReturnToDraft moves an approved, not-yet-started plan back to draft so
// its method and objective can be reassessed after specimen data changed.
func (p *PrepPlan) ReturnToDraft(now time.Time) error {
	if p.Status != PlanApproved {
		return contracts.Conflict("plan_not_returnable", "只有已审批且未开始的方案可以退回草稿重新评估")
	}
	stamp := now.UTC()
	p.Status = PlanDraft
	p.ReevaluationRequired = true
	p.ApprovedBy = ""
	p.ApprovedAt = nil
	p.Version++
	p.UpdatedAt = stamp
	return nil
}

// ConfirmReevaluation clears the re-evaluation flag on a draft without
// approving it; approval remains a separate decision.
func (p *PrepPlan) ConfirmReevaluation(now time.Time) error {
	if p.Status != PlanDraft {
		return contracts.Conflict("plan_not_reevaluable", "只有草稿方案可以确认重新评估")
	}
	if !p.ReevaluationRequired {
		return contracts.Conflict("plan_reevaluation_not_required", "该方案没有待确认的重新评估")
	}
	p.ReevaluationRequired = false
	p.Version++
	p.UpdatedAt = now.UTC()
	return nil
}

func (p *PrepPlan) Approve(approvedBy string, now time.Time) error {
	if p.Status != PlanDraft {
		return contracts.Conflict("plan_not_approvable", "只有草稿方案可以审批")
	}
	if p.ReevaluationRequired {
		return contracts.Conflict("plan_reevaluation_pending", "标本资料已变更，必须先重新评估方案再审批")
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
