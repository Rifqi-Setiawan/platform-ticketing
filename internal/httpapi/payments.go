package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"tribunpass/internal/booking"
	"tribunpass/internal/simulator"
)

func (s *server) pay(w http.ResponseWriter, r *http.Request, session booking.Session) {
	var input struct {
		HoldID  string `json:"holdId"`
		EventID string `json:"eventId"`
	}
	if !jsonBody(w, r, &input) {
		return
	}
	if !uuidPattern.MatchString(input.HoldID) || !keyPattern.MatchString(input.EventID) {
		fail(w, r, 400, "INVALID_REQUEST", "Provide a hold UUID and a stable payment event ID.")
		return
	}
	// The browser can only ask the simulator to sign the price of its own hold.
	hold, err := s.store.GetHold(r.Context(), session.ID, session.BuyerID, input.HoldID)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	result, err := s.deliver(r.Context(), hold, input.EventID)
	if errors.Is(err, errEventConflict) {
		fail(w, r, 409, "EVENT_CONFLICT", "This event ID was already used for a different payment.")
		return
	}
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, 200, result)
}

var errEventConflict = errors.New("payment event conflicts with a prior event")

func (s *server) deliver(ctx context.Context, hold booking.Hold, eventID string) (booking.PaymentResult, error) {
	event := simulator.Event{EventID: eventID, HoldID: hold.HoldID, AmountIDR: hold.PriceIDR, Type: "payment.succeeded"}
	status, body, err := s.simulator.Deliver(ctx, event)
	if err != nil {
		return booking.PaymentResult{}, err
	}
	if status == 409 {
		return booking.PaymentResult{}, errEventConflict
	}
	if status != 200 {
		return booking.PaymentResult{}, fmt.Errorf("callback returned HTTP %d", status)
	}
	var result booking.PaymentResult
	if err := json.Unmarshal(body, &result); err != nil {
		return booking.PaymentResult{}, err
	}
	if result.EventID != eventID || result.HoldID != hold.HoldID || result.Outcome == "" {
		return booking.PaymentResult{}, errors.New("unexpected payment receipt")
	}
	return result, nil
}

func (s *server) callback(w http.ResponseWriter, r *http.Request) {
	raw, ok := bodyBytes(w, r)
	if !ok {
		return
	}
	if !simulator.Verify(s.config.Secret, raw, r.Header.Get(simulator.SignatureHeader)) {
		fail(w, r, 401, "INVALID_SIGNATURE", "Payment callback signature is invalid.")
		return
	}
	var input struct {
		EventID   string `json:"eventId"`
		HoldID    string `json:"holdId"`
		AmountIDR *int64 `json:"amountIdr"`
		Type      string `json:"type"`
	}
	if !decode(raw, &input) || !keyPattern.MatchString(input.EventID) || !uuidPattern.MatchString(input.HoldID) ||
		input.AmountIDR == nil || *input.AmountIDR < 0 || input.Type != "payment.succeeded" {
		fail(w, r, 400, "INVALID_EVENT", "Payment event is malformed or unsupported.")
		return
	}
	event := booking.PaymentEvent{EventID: input.EventID, HoldID: input.HoldID, AmountIDR: *input.AmountIDR, Type: input.Type}
	result, err := s.store.ApplyPayment(r.Context(), event, requestID(r))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	s.config.Logger.Info("payment callback", "request_id", requestID(r), "event_id", event.EventID, "hold_id", event.HoldID,
		"outcome", result.Outcome, "replayed", result.Replayed)
	if result.Outcome == "EVENT_CONFLICT" {
		fail(w, r, 409, result.Outcome, "This event ID was already used for a different payment.")
		return
	}
	writeJSON(w, 200, result)
}
