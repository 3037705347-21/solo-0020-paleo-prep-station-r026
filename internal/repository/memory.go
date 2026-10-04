package repository

import (
	"sort"
	"sync"
	"time"

	"example.com/paleo-prep-station/internal/domain"
)

type state struct {
	specimens  map[string]domain.Specimen
	plans      map[string]domain.PrepPlan
	progress   map[string][]domain.ProgressEntry
	releases   map[string]domain.ReleaseRecord
	amendments map[string]domain.SpecimenAmendment
	receipts   map[string]AmendmentReceipt
}

// AmendmentReceipt stores the deterministic replay of an idempotent amendment
// request, keyed by the client-provided request id.
type AmendmentReceipt struct {
	RequestID   string
	SpecimenID  string
	Fingerprint string
	Amendment   domain.SpecimenAmendment
	Specimen    domain.Specimen
	Plan        *domain.PrepPlan
	CreatedAt   time.Time
}

// NewAmendmentReceipt builds a receipt without exposing its fields as
// mutable storage.
func NewAmendmentReceipt(
	requestID string,
	specimenID string,
	fingerprint string,
	amendment domain.SpecimenAmendment,
	specimen domain.Specimen,
	plan *domain.PrepPlan,
	now time.Time,
) AmendmentReceipt {
	return AmendmentReceipt{
		RequestID:   requestID,
		SpecimenID:  specimenID,
		Fingerprint: fingerprint,
		Amendment:   amendment,
		Specimen:    specimen,
		Plan:        plan,
		CreatedAt:   now,
	}
}

type MemoryRepository struct {
	mu    sync.Mutex
	state state
}

func NewMemory() *MemoryRepository {
	return &MemoryRepository{
		state: state{
			specimens:  make(map[string]domain.Specimen),
			plans:      make(map[string]domain.PrepPlan),
			progress:   make(map[string][]domain.ProgressEntry),
			releases:   make(map[string]domain.ReleaseRecord),
			amendments: make(map[string]domain.SpecimenAmendment),
			receipts:   make(map[string]AmendmentReceipt),
		},
	}
}

func (r *MemoryRepository) Read(fn func(*Mutation) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return fn(cloneMutation(r.state))
}

func (r *MemoryRepository) Mutate(fn func(*Mutation) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	tx := cloneMutation(r.state)
	if err := fn(tx); err != nil {
		return err
	}
	r.state = tx.snapshot()
	return nil
}

func cloneMutation(source state) *Mutation {
	copied := state{
		specimens:  make(map[string]domain.Specimen, len(source.specimens)),
		plans:      make(map[string]domain.PrepPlan, len(source.plans)),
		progress:   make(map[string][]domain.ProgressEntry, len(source.progress)),
		releases:   make(map[string]domain.ReleaseRecord, len(source.releases)),
		amendments: make(map[string]domain.SpecimenAmendment, len(source.amendments)),
		receipts:   make(map[string]AmendmentReceipt, len(source.receipts)),
	}
	for key, value := range source.specimens {
		copied.specimens[key] = value
	}
	for key, value := range source.plans {
		copied.plans[key] = value
	}
	for key, values := range source.progress {
		copied.progress[key] = append([]domain.ProgressEntry(nil), values...)
	}
	for key, value := range source.releases {
		copied.releases[key] = value
	}
	for key, value := range source.amendments {
		copied.amendments[key] = value
	}
	for key, value := range source.receipts {
		copied.receipts[key] = value
	}
	return &Mutation{state: copied}
}

type Mutation struct {
	state state
}

func (m *Mutation) snapshot() state {
	return m.state
}

func (m *Mutation) GetSpecimen(id string) (domain.Specimen, bool) {
	value, ok := m.state.specimens[id]
	return value, ok
}

func (m *Mutation) FindSpecimenByCatalog(catalogNumber string) (domain.Specimen, bool) {
	for _, value := range m.state.specimens {
		if value.CatalogNumber == catalogNumber {
			return value, true
		}
	}
	return domain.Specimen{}, false
}

func (m *Mutation) ListSpecimens() []domain.Specimen {
	values := make([]domain.Specimen, 0, len(m.state.specimens))
	for _, value := range m.state.specimens {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values
}

func (m *Mutation) PutSpecimen(value domain.Specimen) {
	m.state.specimens[value.ID] = value
}

func (m *Mutation) GetPlan(id string) (domain.PrepPlan, bool) {
	value, ok := m.state.plans[id]
	return value, ok
}

func (m *Mutation) ListPlansForSpecimen(specimenID string) []domain.PrepPlan {
	values := make([]domain.PrepPlan, 0)
	for _, value := range m.state.plans {
		if value.SpecimenID == specimenID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].CreatedAt.Equal(values[j].CreatedAt) {
			return values[i].ID < values[j].ID
		}
		return values[i].CreatedAt.Before(values[j].CreatedAt)
	})
	return values
}

func (m *Mutation) PutPlan(value domain.PrepPlan) {
	m.state.plans[value.ID] = value
}

func (m *Mutation) HasOpenPlan(specimenID string) bool {
	for _, value := range m.state.plans {
		if value.SpecimenID == specimenID && value.IsOpen() {
			return true
		}
	}
	return false
}

// OpenPlanForSpecimen returns the single unfinished plan for a specimen.
// Callers rely on the at-most-one-open-plan invariant; the earliest open plan
// is returned deterministically if the invariant is ever violated.
func (m *Mutation) OpenPlanForSpecimen(specimenID string) (domain.PrepPlan, bool) {
	for _, value := range m.ListPlansForSpecimen(specimenID) {
		if value.IsOpen() {
			return value, true
		}
	}
	return domain.PrepPlan{}, false
}

func (m *Mutation) PutAmendment(value domain.SpecimenAmendment) {
	m.state.amendments[value.ID] = value
}

func (m *Mutation) ListAmendmentsForSpecimen(specimenID string) []domain.SpecimenAmendment {
	values := make([]domain.SpecimenAmendment, 0)
	for _, value := range m.state.amendments {
		if value.SpecimenID == specimenID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Sequence != values[j].Sequence {
			return values[i].Sequence < values[j].Sequence
		}
		return values[i].ID < values[j].ID
	})
	return values
}

func (m *Mutation) GetAmendmentReceipt(requestID string) (AmendmentReceipt, bool) {
	value, ok := m.state.receipts[requestID]
	return value, ok
}

func (m *Mutation) PutAmendmentReceipt(value AmendmentReceipt) {
	m.state.receipts[value.RequestID] = value
}

func (m *Mutation) ProgressForPlan(planID string) []domain.ProgressEntry {
	return append([]domain.ProgressEntry(nil), m.state.progress[planID]...)
}

func (m *Mutation) AppendProgress(value domain.ProgressEntry) {
	m.state.progress[value.PlanID] = append(m.state.progress[value.PlanID], value)
}

func (m *Mutation) ReleaseForSpecimen(specimenID string) (domain.ReleaseRecord, bool) {
	for _, value := range m.state.releases {
		if value.SpecimenID == specimenID {
			return value, true
		}
	}
	return domain.ReleaseRecord{}, false
}

func (m *Mutation) PutRelease(value domain.ReleaseRecord) {
	m.state.releases[value.ID] = value
}
