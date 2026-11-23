package httpapi

import (
	"errors"
	"net/http"
	"tribunpass/internal/booking"
)

func (s *server) adminOverview(w http.ResponseWriter, r *http.Request, session booking.Session) {
	out, err := s.store.AdminOverview(r.Context(), session)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) setCapacity(w http.ResponseWriter, r *http.Request, session booking.Session) {
	sectionID := r.PathValue("id")
	if !uuidPattern.MatchString(sectionID) {
		fail(w, r, 404, "NOT_FOUND", "Resource not found.")
		return
	}
	var input struct {
		Capacity int `json:"capacity"`
	}
	if !jsonBody(w, r, &input) {
		return
	}
	if input.Capacity < 1 || input.Capacity > 1000 {
		fail(w, r, 400, "INVALID_CAPACITY", "Set a capacity between 1 and 1000.")
		return
	}
	result, err := s.store.SetCapacity(r.Context(), session, sectionID, input.Capacity)
	if errors.Is(err, booking.ErrCapacityBelowAllocated) {
		fail(w, r, 409, "CAPACITY_BELOW_ALLOCATED", "Capacity must cover all held and sold tickets.")
		return
	}
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, 200, result)
}
