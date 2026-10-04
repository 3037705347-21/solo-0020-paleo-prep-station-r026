package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

type SpecimenStatus string

const (
	SpecimenRegistered SpecimenStatus = "registered"
	SpecimenPlanned    SpecimenStatus = "planned"
	SpecimenPreparing  SpecimenStatus = "preparing"
	SpecimenStabilized SpecimenStatus = "stabilized"
	SpecimenAvailable  SpecimenStatus = "available"
)

type Specimen struct {
	ID            string         `json:"id"`
	CatalogNumber string         `json:"catalog_number"`
	FieldLabel    string         `json:"field_label"`
	TaxonHint     string         `json:"taxon_hint"`
	MatrixKind    string         `json:"matrix_kind"`
	JacketNotes   string         `json:"jacket_notes"`
	Status        SpecimenStatus `json:"status"`
	Version       int            `json:"version"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

func NewSpecimen(
	id string,
	catalogNumber string,
	fieldLabel string,
	taxonHint string,
	matrixKind string,
	jacketNotes string,
	now time.Time,
) (Specimen, error) {
	if id == "" {
		return Specimen{}, contracts.Internal("missing_generated_id", "标本标识未生成", nil)
	}
	catalog, err := validateCatalogNumber(catalogNumber)
	if err != nil {
		return Specimen{}, err
	}
	field, err := requiredText("野外标签", fieldLabel, 80)
	if err != nil {
		return Specimen{}, err
	}
	taxon, err := optionalText("分类提示", taxonHint, 120)
	if err != nil {
		return Specimen{}, err
	}
	matrix, err := requiredText("基质类型", matrixKind, 60)
	if err != nil {
		return Specimen{}, err
	}
	notes, err := optionalText("夹克备注", jacketNotes, 500)
	if err != nil {
		return Specimen{}, err
	}
	return Specimen{
		ID:            id,
		CatalogNumber: catalog,
		FieldLabel:    field,
		TaxonHint:     taxon,
		MatrixKind:    matrix,
		JacketNotes:   notes,
		Status:        SpecimenRegistered,
		Version:       1,
		CreatedAt:     now.UTC(),
		UpdatedAt:     now.UTC(),
	}, nil
}

// AmendInput carries a partial correction of registration-time data.
// Pointer fields distinguish "field omitted" from "field cleared".
type AmendInput struct {
	TaxonHint   *string
	MatrixKind  *string
	JacketNotes *string
}

// AmendImpact describes whether a correction touches the assumptions
// behind the current method and objective.
type AmendImpact struct {
	TaxonHintChanged   bool
	MatrixKindChanged  bool
	JacketNotesChanged bool
}

func (i AmendImpact) AffectsPlan() bool {
	return i.TaxonHintChanged || i.MatrixKindChanged
}

func (i AmendImpact) Any() bool {
	return i.TaxonHintChanged || i.MatrixKindChanged || i.JacketNotesChanged
}

// ChangedFields lists the JSON field names touched by the amendment.
func (i AmendImpact) ChangedFields() []string {
	fields := make([]string, 0, 3)
	if i.TaxonHintChanged {
		fields = append(fields, "taxon_hint")
	}
	if i.MatrixKindChanged {
		fields = append(fields, "matrix_kind")
	}
	if i.JacketNotesChanged {
		fields = append(fields, "jacket_notes")
	}
	return fields
}

func (s Specimen) CanCreatePlan() bool {
	return s.Status == SpecimenRegistered || s.Status == SpecimenPlanned
}

func (s Specimen) IsStable() bool {
	return s.Status == SpecimenStabilized
}

// CanAmend reports whether preparation is still open for corrections.
// Stabilized and released specimens are frozen.
func (s Specimen) CanAmend() bool {
	return s.Status == SpecimenRegistered ||
		s.Status == SpecimenPlanned ||
		s.Status == SpecimenPreparing
}

// Amend applies a correction with reason. It validates input the same way
// registration does and bumps the version only when something changed.
func (s *Specimen) Amend(input AmendInput, now time.Time) (AmendImpact, error) {
	if !s.CanAmend() {
		switch s.Status {
		case SpecimenStabilized:
			return AmendImpact{}, contracts.Conflict("specimen_locked_stabilized", "标本已稳定，不能直接改写登记资料")
		default:
			return AmendImpact{}, contracts.Conflict("specimen_locked_released", "标本已放行，资料永久冻结")
		}
	}

	next := *s
	impact := AmendImpact{}

	if input.TaxonHint != nil {
		value, err := optionalText("分类提示", *input.TaxonHint, 120)
		if err != nil {
			return AmendImpact{}, err
		}
		if value != s.TaxonHint {
			next.TaxonHint = value
			impact.TaxonHintChanged = true
		}
	}
	if input.MatrixKind != nil {
		value, err := requiredText("基质类型", *input.MatrixKind, 60)
		if err != nil {
			return AmendImpact{}, err
		}
		if value != s.MatrixKind {
			next.MatrixKind = value
			impact.MatrixKindChanged = true
		}
	}
	if input.JacketNotes != nil {
		value, err := optionalText("夹克备注", *input.JacketNotes, 500)
		if err != nil {
			return AmendImpact{}, err
		}
		if value != s.JacketNotes {
			next.JacketNotes = value
			impact.JacketNotesChanged = true
		}
	}
	if !impact.Any() {
		return AmendImpact{}, contracts.Conflict("no_specimen_change", "提交内容与当前资料一致，未产生修改")
	}
	next.Version = s.Version + 1
	next.UpdatedAt = now.UTC()
	*s = next
	return impact, nil
}

func (s *Specimen) MarkPlanned(now time.Time) error {
	if s.Status == SpecimenPlanned {
		return nil
	}
	if s.Status != SpecimenRegistered {
		return contracts.Conflict("specimen_not_plannable", "当前标本状态不能创建修复方案")
	}
	s.Status = SpecimenPlanned
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}

func (s *Specimen) MarkPreparing(now time.Time) error {
	if s.Status == SpecimenPreparing {
		return nil
	}
	if s.Status != SpecimenPlanned {
		return contracts.Conflict("specimen_not_preparing", "只有已规划标本才能进入制备状态")
	}
	s.Status = SpecimenPreparing
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}

func (s *Specimen) MarkStabilized(now time.Time) error {
	if s.Status == SpecimenStabilized {
		return nil
	}
	if s.Status != SpecimenPreparing {
		return contracts.Conflict("specimen_not_stabilizable", "只有正在制备的标本才能进入稳定状态")
	}
	s.Status = SpecimenStabilized
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}

func (s *Specimen) MarkAvailable(now time.Time) error {
	if s.Status != SpecimenStabilized {
		return contracts.Conflict("specimen_not_releasable", "只有已稳定标本可以办理放行")
	}
	s.Status = SpecimenAvailable
	s.Version++
	s.UpdatedAt = now.UTC()
	return nil
}
