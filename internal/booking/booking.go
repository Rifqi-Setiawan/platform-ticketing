package booking

import (
	"context"
	"time"
)

type HoldStatus string

const (
	Held      HoldStatus = "HELD"
	Confirmed HoldStatus = "CONFIRMED"
	Expired   HoldStatus = "EXPIRED"
)

func DecidePayment(status HoldStatus, expiresAt, dbNow time.Time) string {
	switch status {
	case Confirmed:
		return "ALREADY_CONFIRMED"
	case Expired:
		return "RECORDED_LATE"
	case Held:
		if dbNow.Before(expiresAt) {
			return "CONFIRMED"
		}
		return "RECORDED_LATE"
	default:
		panic("invalid hold status")
	}
}

type Session struct{ ID, BuyerID string }
type Event struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Sections []Section `json:"sections"`
}
type Section struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Capacity  int    `json:"capacity"`
	Available int    `json:"available"`
	PriceIDR  int64  `json:"priceIdr"`
}
type CreateHoldCommand struct{ SessionID, BuyerID, SectionID, Key, RequestID string }
type CreateHoldResult struct {
	Code      string     `json:"-"`
	HoldID    string     `json:"holdId"`
	Status    HoldStatus `json:"status"`
	PriceIDR  int64      `json:"priceIdr"`
	ExpiresAt time.Time  `json:"expiresAt"`
}
type Transition struct {
	Sequence int        `json:"sequence"`
	From     HoldStatus `json:"from,omitempty"`
	To       HoldStatus `json:"to"`
	Cause    string     `json:"cause"`
	At       time.Time  `json:"at"`
}
type Hold struct {
	HoldID      string       `json:"holdId"`
	SectionID   string       `json:"sectionId"`
	Status      HoldStatus   `json:"status"`
	PriceIDR    int64        `json:"priceIdr"`
	ExpiresAt   time.Time    `json:"expiresAt"`
	TicketID    string       `json:"ticketId,omitempty"`
	Transitions []Transition `json:"transitions"`
}
type PaymentEvent struct {
	EventID   string `json:"eventId"`
	HoldID    string `json:"holdId"`
	AmountIDR int64  `json:"amountIdr"`
	Type      string `json:"type"`
}
type PaymentResult struct {
	Replayed bool   `json:"-"`
	EventID  string `json:"eventId"`
	HoldID   string `json:"holdId"`
	Outcome  string `json:"outcome"`
	TicketID string `json:"ticketId,omitempty"`
}
type Fixture struct{ SectionID, BuyerA, BuyerB, HoldID string }
type Store interface {
	CreateSession(ctx context.Context, tokenHash string) (Session, error)
	FindSession(ctx context.Context, tokenHash string) (Session, error)
	Events(ctx context.Context, sessionID string) ([]Event, error)
	CreateHold(ctx context.Context, cmd CreateHoldCommand) (CreateHoldResult, error)
	GetHold(ctx context.Context, sessionID, buyerID, holdID string) (Hold, error)
	ApplyPayment(ctx context.Context, event PaymentEvent, requestID string) (PaymentResult, error)
	ExpireNext(ctx context.Context) (bool, error)
	CreateFixture(ctx context.Context, session Session, kind string) (Fixture, error)
	Section(ctx context.Context, sessionID, sectionID string) (Section, error)
}
