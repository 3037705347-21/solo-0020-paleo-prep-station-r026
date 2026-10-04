package station

import (
	"context"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

// AmendResult is the deterministic outcome of one amendment submit.
type AmendResult struct {
	Specimen domain.Specimen         `json:"specimen"`
	Revision domain.SpecimenRevision `json:"revision"`
	// PlanImpact reports whether the correction touched objective/method
	// assumptions of the current plan.
	PlanImpact bool `json:"plan_impact"`
	// PlanReset is true when an approved plan was returned to draft.
	PlanReset bool `json:"plan_reset"`
	// ReevaluationRequired is true when a draft plan was flagged for review.
	ReevaluationRequired bool             `json:"reevaluation_required"`
	Plan                 *domain.PrepPlan `json:"plan,omitempty"`
}

func (s *Service) AmendSpecimen(
	ctx context.Context,
	specimenID string,
	request contracts.AmendSpecimenRequest,
) (AmendResult, error) {
	if err := ctx.Err(); err != nil {
		return AmendResult{}, err
	}

	var result AmendResult
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		specimen, ok := m.GetSpecimen(specimenID)
		if !ok {
			return notFoundSpecimen(specimenID)
		}

		// Optimistic concurrency check. Both genuine concurrent edits and
		// duplicate replays of an already applied submit land here.
		if request.ExpectedVersion <= 0 {
			return contracts.Invalid("missing_expected_version", "必须提供 expected_version 以确认基于哪个版本修改")
		}
		if request.ExpectedVersion != specimen.Version {
			return contracts.Conflict(
				"specimen_version_conflict",
				"标本资料已被其他修改更新，请基于最新版本重新提交",
			)
		}

		reason, err := domain.ValidateRevisionReason(request.Reason)
		if err != nil {
			return err
		}

		input := domain.AmendInput{
			TaxonHint:   request.TaxonHint,
			MatrixKind:  request.MatrixKind,
			JacketNotes: request.JacketNotes,
		}
		fromVersion := specimen.Version
		impact, err := specimen.Amend(input, s.now())
		if err != nil {
			return err
		}

		result.PlanImpact = impact.AffectsPlan()

		planID := ""
		planReset := false
		reevaluationRequired := false
		if openPlan, hasOpen := m.OpenPlanForSpecimen(specimenID); hasOpen {
			planID = openPlan.ID
			if impact.AffectsPlan() {
				switch openPlan.Status {
				case domain.PlanDraft:
					if err := openPlan.MarkReevaluationRequired(s.now()); err != nil {
						return err
					}
					reevaluationRequired = true
				case domain.PlanApproved:
					if !request.ForceReevaluation {
						return contracts.Conflict(
							"plan_reevaluation_required",
							"修改影响当前方案的方法与目标，必须退回草稿重新评估后才能生效",
						)
					}
					if err := openPlan.ReturnToDraft(s.now()); err != nil {
						return err
					}
					planReset = true
					reevaluationRequired = true
				default:
					// PlanActive: preparation has started, data is frozen.
					return contracts.Conflict(
						"plan_in_progress_locked",
						"方案已进入制备，影响方法与目标的资料不能直接修改",
					)
				}
			}
			if reevaluationRequired || planReset {
				planCopy := openPlan
				result.Plan = &planCopy
				m.PutPlan(openPlan)
			}
		}

		revisions := m.RevisionsForSpecimen(specimenID)
		revision, err := domain.NewSpecimenRevision(
			s.nextID("revision"),
			specimen,
			len(revisions),
			fromVersion,
			impact,
			reason,
			planID,
			planReset,
			s.now(),
		)
		if err != nil {
			return err
		}

		m.PutSpecimen(specimen)
		m.AppendRevision(revision)

		result.Specimen = specimen
		result.Revision = revision
		result.PlanReset = planReset
		result.ReevaluationRequired = reevaluationRequired
		return nil
	})
	return result, err
}

func (s *Service) ListSpecimenRevisions(
	ctx context.Context,
	specimenID string,
) ([]domain.SpecimenRevision, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.SpecimenRevision
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetSpecimen(specimenID); !ok {
			return notFoundSpecimen(specimenID)
		}
		values = m.RevisionsForSpecimen(specimenID)
		return nil
	})
	return values, err
}

func (s *Service) ConfirmPlanReevaluation(
	ctx context.Context,
	planID string,
	request contracts.ReevaluationRequest,
) (domain.PrepPlan, error) {
	if err := ctx.Err(); err != nil {
		return domain.PrepPlan{}, err
	}

	var updated domain.PrepPlan
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		plan, ok := m.GetPlan(planID)
		if !ok {
			return notFoundPlan(planID)
		}
		if _, err := domain.ValidateActorName("确认人", request.ConfirmedBy, 80); err != nil {
			return err
		}
		if err := plan.ConfirmReevaluation(s.now()); err != nil {
			return err
		}
		m.PutPlan(plan)
		updated = plan
		return nil
	})
	return updated, err
}
