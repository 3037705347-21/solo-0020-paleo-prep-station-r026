package contracts

type RegisterSpecimenRequest struct {
	CatalogNumber string `json:"catalog_number"`
	FieldLabel    string `json:"field_label"`
	TaxonHint     string `json:"taxon_hint"`
	MatrixKind    string `json:"matrix_kind"`
	JacketNotes   string `json:"jacket_notes"`
}

type CreatePlanRequest struct {
	Objective      string `json:"objective"`
	Method         string `json:"method"`
	Adhesive       string `json:"adhesive"`
	TargetExposure int    `json:"target_exposure"`
}

type ApprovalRequest struct {
	ApprovedBy string `json:"approved_by"`
}

type ProgressRequest struct {
	Stage      string `json:"stage"`
	Percent    int    `json:"percent"`
	Note       string `json:"note"`
	RecordedBy string `json:"recorded_by"`
}

type ReleaseRequest struct {
	Purpose    string `json:"purpose"`
	Recipient  string `json:"recipient"`
	ApprovedBy string `json:"approved_by"`
}

// AmendSpecimenRequest carries a partial correction of specimen data.
// Nil fields are left untouched. ExpectedVersion implements optimistic
// concurrency: a stale or replayed submit must be rejected deterministically.
type AmendSpecimenRequest struct {
	TaxonHint       *string `json:"taxon_hint"`
	MatrixKind      *string `json:"matrix_kind"`
	JacketNotes     *string `json:"jacket_notes"`
	Reason          string  `json:"reason"`
	ExpectedVersion int     `json:"expected_version"`
	// ForceReevaluation allows moving an approved, not-started plan back to
	// draft when the correction affects its objective or method.
	ForceReevaluation bool `json:"force_reevaluation"`
}

// ReevaluationRequest records who confirmed that the draft plan has been
// reassessed against the corrected specimen data.
type ReevaluationRequest struct {
	ConfirmedBy string `json:"confirmed_by"`
}
