package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

// SpecimenRevision keeps one immutable correction of specimen data,
// including the version it produced and why the change was requested.
type SpecimenRevision struct {
	ID            string    `json:"id"`
	SpecimenID    string    `json:"specimen_id"`
	Sequence      int       `json:"sequence"`
	FromVersion   int       `json:"from_version"`
	ToVersion     int       `json:"to_version"`
	TaxonHint     *string   `json:"taxon_hint,omitempty"`
	MatrixKind    *string   `json:"matrix_kind,omitempty"`
	JacketNotes   *string   `json:"jacket_notes,omitempty"`
	ChangedFields []string  `json:"changed_fields"`
	AffectedPlan  bool      `json:"affected_plan"`
	Reason        string    `json:"reason"`
	PlanID        string    `json:"plan_id,omitempty"`
	PlanReset     bool      `json:"plan_reset"`
	CreatedAt     time.Time `json:"created_at"`
}

// NewSpecimenRevision constructs a revision from the specimen snapshot
// after a successful amendment. previousCount determines the sequence.
func NewSpecimenRevision(
	id string,
	specimen Specimen,
	previousCount int,
	fromVersion int,
	impact AmendImpact,
	reason string,
	planID string,
	planReset bool,
	now time.Time,
) (SpecimenRevision, error) {
	if id == "" {
		return SpecimenRevision{}, contracts.Internal("missing_generated_id", "修订标识未生成", nil)
	}
	reasonValue, err := requiredText("修改原因", reason, 300)
	if err != nil {
		return SpecimenRevision{}, err
	}
	if !impact.Any() {
		return SpecimenRevision{}, contracts.Internal("empty_revision", "修订记录必须至少包含一项变更", nil)
	}
	return SpecimenRevision{
		ID:            id,
		SpecimenID:    specimen.ID,
		Sequence:      previousCount + 1,
		FromVersion:   fromVersion,
		ToVersion:     specimen.Version,
		TaxonHint:     valueOrNil(impact.TaxonHintChanged, specimen.TaxonHint),
		MatrixKind:    valueOrNil(impact.MatrixKindChanged, specimen.MatrixKind),
		JacketNotes:   valueOrNil(impact.JacketNotesChanged, specimen.JacketNotes),
		ChangedFields: impact.ChangedFields(),
		AffectedPlan:  impact.AffectsPlan(),
		Reason:        reasonValue,
		PlanID:        planID,
		PlanReset:     planReset,
		CreatedAt:     now.UTC(),
	}, nil
}

func valueOrNil(changed bool, value string) *string {
	if !changed {
		return nil
	}
	v := value
	return &v
}
