package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"tribunpass/internal/booking"
)

type scenarioStep struct {
	Label    string `json:"label"`
	Outcome  string `json:"outcome"`
	HoldID   string `json:"holdId,omitempty"`
	TicketID string `json:"ticketId,omitempty"`
	EventID  string `json:"eventId,omitempty"`
}

type scenarioRun struct {
	Scenario  string         `json:"scenario"`
	SectionID string         `json:"sectionId"`
	Capacity  int            `json:"capacity"`
	Available int            `json:"available"`
	Steps     []scenarioStep `json:"steps"`
}

func (s *server) scenario(w http.ResponseWriter, r *http.Request, session booking.Session) {
	var input struct {
		Scenario string `json:"scenario"`
	}
	if !jsonBody(w, r, &input) {
		return
	}
	switch input.Scenario {
	case "last-ticket", "duplicate-payment", "late-payment":
	default:
		fail(w, r, 400, "UNKNOWN_SCENARIO", "Choose last-ticket, duplicate-payment, or late-payment.")
		return
	}
	fixture, err := s.store.CreateFixture(r.Context(), session, input.Scenario)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	var steps []scenarioStep
	if input.Scenario == "last-ticket" {
		steps, err = s.race(r.Context(), session, fixture, requestID(r))
	} else {
		steps, err = s.paymentScenario(r.Context(), session, fixture, input.Scenario, requestID(r))
	}
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	section, err := s.store.Section(r.Context(), session.ID, fixture.SectionID)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, 200, scenarioRun{input.Scenario, section.ID, section.Capacity, section.Available, steps})
}

func (s *server) scenarioHold(ctx context.Context, sessionID, buyerID, sectionID, requestID string) (booking.CreateHoldResult, error) {
	return s.store.CreateHold(ctx, booking.CreateHoldCommand{
		SessionID: sessionID, BuyerID: buyerID, SectionID: sectionID, Key: randomID(), RequestID: requestID,
	})
}

// race releases two buyers at a shared barrier and waits for both requests. The
// API accepts a fixed scenario name, so the fanout stays bounded per scenario.
func (s *server) race(ctx context.Context, session booking.Session, fixture booking.Fixture, trace string) ([]scenarioStep, error) {
	type attempt struct {
		idx    int
		result booking.CreateHoldResult
		err    error
	}
	start := make(chan struct{})
	done := make(chan attempt, 2)
	for i, buyer := range []string{fixture.BuyerA, fixture.BuyerB} {
		go func(i int, buyer string) {
			<-start
			result, err := s.scenarioHold(ctx, session.ID, buyer, fixture.SectionID, trace)
			done <- attempt{i, result, err}
		}(i, buyer)
	}
	close(start)
	steps := make([]scenarioStep, 2)
	var firstError error
	labels := []string{"Buyer A requested the final ticket", "Buyer B requested the final ticket"}
	for range 2 {
		attempt := <-done
		if attempt.err != nil && firstError == nil {
			firstError = attempt.err
		}
		steps[attempt.idx] = scenarioStep{Label: labels[attempt.idx], Outcome: attempt.result.Code, HoldID: attempt.result.HoldID}
	}
	return steps, firstError
}

func (s *server) paymentScenario(ctx context.Context, session booking.Session, fixture booking.Fixture, kind, trace string) ([]scenarioStep, error) {
	var steps []scenarioStep
	holdID := fixture.HoldID
	if kind == "duplicate-payment" {
		created, err := s.scenarioHold(ctx, session.ID, fixture.BuyerA, fixture.SectionID, trace)
		if err != nil {
			return nil, err
		}
		if created.Code != "CREATED" {
			return nil, fmt.Errorf("fresh scenario hold: %s", created.Code)
		}
		holdID = created.HoldID
		steps = append(steps, scenarioStep{Label: "Reserve the ticket", Outcome: created.Code, HoldID: holdID})
	} else {
		steps = append(steps, scenarioStep{Label: "Start with a hold past its deadline", Outcome: "DEADLINE_PASSED", HoldID: holdID})
	}
	hold, err := s.store.GetHold(ctx, session.ID, fixture.BuyerA, holdID)
	if err != nil {
		return nil, err
	}
	event := randomID()
	labels := []string{"Deliver the payment callback"}
	if kind == "duplicate-payment" {
		labels = append(labels, "Redeliver the same event")
	}
	for _, label := range labels {
		receipt, err := s.deliver(ctx, hold, event)
		if err != nil {
			return nil, err
		}
		steps = append(steps, scenarioStep{Label: label, Outcome: receipt.Outcome, HoldID: holdID, TicketID: receipt.TicketID, EventID: event})
	}
	return steps, nil
}
