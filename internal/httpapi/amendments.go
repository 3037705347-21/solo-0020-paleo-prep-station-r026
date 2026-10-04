package httpapi

import (
	"net/http"

	"example.com/paleo-prep-station/internal/contracts"
)

func (api *API) amendSpecimen(w http.ResponseWriter, r *http.Request) {
	var request contracts.AmendSpecimenRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.AmendSpecimen(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (api *API) listSpecimenAmendments(w http.ResponseWriter, r *http.Request) {
	values, err := api.service.ListSpecimenAmendments(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (api *API) resolvePlanReevaluation(w http.ResponseWriter, r *http.Request) {
	var request contracts.ReevaluationRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.ResolvePlanReevaluation(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
