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
	result, err := api.service.AmendSpecimen(r.Context(), r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) listSpecimenRevisions(w http.ResponseWriter, r *http.Request) {
	values, err := api.service.ListSpecimenRevisions(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}
