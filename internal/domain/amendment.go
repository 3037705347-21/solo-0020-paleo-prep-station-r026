package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

// SpecimenField identifies an amendable specimen attribute.
type SpecimenField string

const (
	FieldTaxonHint   SpecimenField = "taxon_hint"
	FieldMatrixKind  SpecimenField = "matrix_kind"
	FieldJacketNotes SpecimenField = "jacket_notes"
)

// AmendmentInput carries a single, reason-bearing correction request for the
// three registrable attributes that may turn out wrong before preparation ends.
type AmendmentInput struct {
	TaxonHint       *string
	MatrixKind      *string
	JacketNotes     *string
	Reason          string
	ExpectedVersion int
}

// SpecimenAmendment is the permanently retained record of one correction.
type SpecimenAmendment struct {
	ID              string          `json:"id"`
	SpecimenID      string          `json:"specimen_id"`
	Sequence        int             `json:"sequence"`
	ChangedFields   []SpecimenField `json:"changed_fields"`
	TaxonHintBefore string          `json:"taxon_hint_before"`
	TaxonHintAfter  string          `json:"taxon_hint_after"`
	MatrixBefore    string          `json:"matrix_kind_before"`
	MatrixAfter     string          `json:"matrix_kind_after"`
	JacketBefore    string          `json:"jacket_notes_before"`
	JacketAfter     string          `json:"jacket_notes_after"`
	Reason          string          `json:"reason"`
	// AffectsPlan is true when the correction forced the open draft plan to be
	// reassessed before it can be approved.
	AffectsPlan bool      `json:"affects_plan"`
	PlanID      string    `json:"plan_id,omitempty"`
	BaseVersion int       `json:"base_version"`
	NewVersion  int       `json:"new_version"`
	CreatedAt   time.Time `json:"created_at"`
}

// AffectsMethodOrTarget reports whether the corrected fields change the
// assumptions that the current preparation method and objective rely on.
//
// 基质类型决定剥离方法与黏结材料是否适用；分类提示决定暴露目标的取舍；
// 夹克备注只是搬运保护信息，不影响方法与目标。
func AffectsMethodOrTarget(fields []SpecimenField) bool {
	for _, field := range fields {
		if field == FieldMatrixKind || field == FieldTaxonHint {
			return true
		}
	}
	return false
}

// Amend applies a validated correction to a specimen that has not finished
// preparation. It returns the retained amendment record; the receiver is only
// mutated when every check passes.
func (s *Specimen) Amend(
	id string,
	sequence int,
	input AmendmentInput,
	now time.Time,
) (SpecimenAmendment, error) {
	if id == "" {
		return SpecimenAmendment{}, contracts.Internal("missing_generated_id", "更正标识未生成", nil)
	}
	if !s.CanAmend() {
		return SpecimenAmendment{}, contracts.Conflict(
			"specimen_not_amendable",
			"已稳定或已放行的标本不能直接改写资料",
		)
	}
	if input.ExpectedVersion <= 0 {
		return SpecimenAmendment{}, contracts.Invalid(
			"invalid_expected_version",
			"expected_version 必须是正整数",
		)
	}
	if input.ExpectedVersion != s.Version {
		return SpecimenAmendment{}, contracts.Conflict(
			"specimen_version_conflict",
			"标本资料已被其他修改更新，请基于最新版本重试",
		)
	}
	if input.TaxonHint == nil && input.MatrixKind == nil && input.JacketNotes == nil {
		return SpecimenAmendment{}, contracts.Invalid(
			"no_amendable_field",
			"必须至少提供分类提示、基质类型或夹克备注中的一项",
		)
	}
	reason, err := requiredText("更正原因", input.Reason, 300)
	if err != nil {
		return SpecimenAmendment{}, err
	}

	taxon := s.TaxonHint
	matrix := s.MatrixKind
	jacket := s.JacketNotes
	changed := make([]SpecimenField, 0, 3)
	amendment := SpecimenAmendment{
		ID:              id,
		SpecimenID:      s.ID,
		Sequence:        sequence,
		Reason:          reason,
		TaxonHintBefore: s.TaxonHint,
		MatrixBefore:    s.MatrixKind,
		JacketBefore:    s.JacketNotes,
		BaseVersion:     s.Version,
		CreatedAt:       now.UTC(),
	}

	if input.TaxonHint != nil {
		value, err := optionalText("分类提示", *input.TaxonHint, 120)
		if err != nil {
			return SpecimenAmendment{}, err
		}
		if value != taxon {
			taxon = value
			changed = append(changed, FieldTaxonHint)
		}
	}
	if input.MatrixKind != nil {
		value, err := requiredText("基质类型", *input.MatrixKind, 60)
		if err != nil {
			return SpecimenAmendment{}, err
		}
		if value != matrix {
			matrix = value
			changed = append(changed, FieldMatrixKind)
		}
	}
	if input.JacketNotes != nil {
		value, err := optionalText("夹克备注", *input.JacketNotes, 500)
		if err != nil {
			return SpecimenAmendment{}, err
		}
		if value != jacket {
			jacket = value
			changed = append(changed, FieldJacketNotes)
		}
	}

	if len(changed) == 0 {
		return SpecimenAmendment{}, contracts.Conflict(
			"specimen_unchanged",
			"提交内容与当前标本资料一致，没有可更正的变化",
		)
	}

	amendment.ChangedFields = changed
	if input.TaxonHint != nil {
		amendment.TaxonHintAfter = taxon
	}
	if input.MatrixKind != nil {
		amendment.MatrixAfter = matrix
	}
	if input.JacketNotes != nil {
		amendment.JacketAfter = jacket
	}

	stamp := now.UTC()
	s.TaxonHint = taxon
	s.MatrixKind = matrix
	s.JacketNotes = jacket
	s.Version++
	s.UpdatedAt = stamp
	amendment.NewVersion = s.Version
	return amendment, nil
}
