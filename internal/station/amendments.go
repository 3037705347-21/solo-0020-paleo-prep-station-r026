package station

import (
	"context"
	"encoding/json"
	"regexp"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,79}$`)

// AmendmentResult is the committed outcome of one correction, including any
// draft plan that was forced into reevaluation.
type AmendmentResult struct {
	Specimen  domain.Specimen          `json:"specimen"`
	Amendment domain.SpecimenAmendment `json:"amendment"`
	Plan      *domain.PrepPlan         `json:"plan,omitempty"`
}

func (s *Service) AmendSpecimen(
	ctx context.Context,
	specimenID string,
	request contracts.AmendSpecimenRequest,
) (AmendmentResult, error) {
	if err := ctx.Err(); err != nil {
		return AmendmentResult{}, err
	}

	var result AmendmentResult
	err := s.repo.Mutate(func(m *repository.Mutation) error {
		specimen, ok := m.GetSpecimen(specimenID)
		if !ok {
			return notFoundSpecimen(specimenID)
		}

		fingerprint := amendmentFingerprint(request)
		if request.RequestID != "" {
			if !requestIDPattern.MatchString(request.RequestID) {
				return contracts.Invalid(
					"invalid_request_id",
					"request_id 只能包含字母、数字、点、下划线和连字符",
				)
			}
			if receipt, exists := m.GetAmendmentReceipt(request.RequestID); exists {
				if receipt.SpecimenID != specimenID || receipt.Fingerprint != fingerprint {
					return contracts.Conflict(
						"idempotency_key_reused",
						"request_id 已用于其他更正请求，不能复用",
					)
				}
				// Deterministic replay of an identical retried request:
				// return the original committed outcome unchanged.
				result = AmendmentResult{
					Specimen:  receipt.Specimen,
					Amendment: receipt.Amendment,
					Plan:      receipt.Plan,
				}
				return nil
			}
		}

		amendment, err := specimen.Amend(
			s.nextID("amendment"),
			len(m.ListAmendmentsForSpecimen(specimenID))+1,
			domain.AmendmentInput{
				TaxonHint:       request.TaxonHint,
				MatrixKind:      request.MatrixKind,
				JacketNotes:     request.JacketNotes,
				Reason:          request.Reason,
				ExpectedVersion: request.ExpectedVersion,
			},
			s.now(),
		)
		if err != nil {
			return err
		}

		var affectedPlan *domain.PrepPlan
		if open, hasOpen := m.OpenPlanForSpecimen(specimenID); hasOpen {
			if domain.AffectsMethodOrTarget(amendment.ChangedFields) {
				if open.Status != domain.PlanDraft {
					// Approved or active plans already encode a method and
					// exposure target built on the corrected data; rewriting
					// underneath them would invalidate the plan and the
					// release basis, so the correction is blocked outright.
					return contracts.Conflict(
						"plan_references_specimen_data",
						"当前修改影响已审批或进行中方案的方法与目标，请先完成或取消方案后再修改",
					)
				}
				if err := open.MarkReevaluationRequired(s.now()); err != nil {
					return err
				}
				amendment.AffectsPlan = true
				amendment.PlanID = open.ID
				m.PutPlan(open)
				planCopy := open
				affectedPlan = &planCopy
			}
		}

		m.PutSpecimen(specimen)
		m.PutAmendment(amendment)
		if request.RequestID != "" {
			m.PutAmendmentReceipt(repository.NewAmendmentReceipt(
				request.RequestID,
				specimenID,
				fingerprint,
				amendment,
				specimen,
				affectedPlan,
				s.now(),
			))
		}
		result = AmendmentResult{
			Specimen:  specimen,
			Amendment: amendment,
			Plan:      affectedPlan,
		}
		return nil
	})
	return result, err
}

func (s *Service) ListSpecimenAmendments(
	ctx context.Context,
	specimenID string,
) ([]domain.SpecimenAmendment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var values []domain.SpecimenAmendment
	err := s.repo.Read(func(m *repository.Mutation) error {
		if _, ok := m.GetSpecimen(specimenID); !ok {
			return notFoundSpecimen(specimenID)
		}
		values = m.ListAmendmentsForSpecimen(specimenID)
		return nil
	})
	return values, err
}

func (s *Service) ResolvePlanReevaluation(
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
		if err := plan.ResolveReevaluation(request.ReviewedBy, request.Note, s.now()); err != nil {
			return err
		}
		m.PutPlan(plan)
		updated = plan
		return nil
	})
	return updated, err
}

// amendmentFingerprint deterministically captures the amendment intent so a
// replayed request id can be told apart from a reused one. Only the request
// id itself is excluded because it is the deduplication key.
func amendmentFingerprint(request contracts.AmendSpecimenRequest) string {
	payload := struct {
		TaxonHint       *string `json:"taxon_hint"`
		MatrixKind      *string `json:"matrix_kind"`
		JacketNotes     *string `json:"jacket_notes"`
		Reason          string  `json:"reason"`
		ExpectedVersion int     `json:"expected_version"`
	}{
		TaxonHint:       request.TaxonHint,
		MatrixKind:      request.MatrixKind,
		JacketNotes:     request.JacketNotes,
		Reason:          request.Reason,
		ExpectedVersion: request.ExpectedVersion,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		// The struct contains only strings and ints; marshalling cannot fail.
		return ""
	}
	return string(encoded)
}
