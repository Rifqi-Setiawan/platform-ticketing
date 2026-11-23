package booking

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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
	StartsAt time.Time `json:"startsAt"`
	Sections []Section `json:"sections"`
}
type Section struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Capacity  int    `json:"capacity"`
	Available int    `json:"available"`
	PriceIDR  int64  `json:"priceIdr"`
}
type CreateHoldCommand struct {
	SessionID, BuyerID, SectionID, Key, RequestID string
	Quantity                                      int
	DisplayName                                   string
}

func ValidOrder(quantity int, name string) bool {
	return quantity >= 1 && quantity <= 4 && utf8.RuneCountInString(name) <= 60 && strings.IndexFunc(name, unicode.IsControl) < 0
}

type CreateHoldResult struct {
	Code        string     `json:"-"`
	HoldID      string     `json:"holdId"`
	Status      HoldStatus `json:"status"`
	PriceIDR    int64      `json:"priceIdr"`
	Quantity    int        `json:"quantity"`
	TotalIDR    int64      `json:"totalIdr"`
	DisplayName string     `json:"displayName"`
	ExpiresAt   time.Time  `json:"expiresAt"`
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
	Quantity    int          `json:"quantity"`
	TotalIDR    int64        `json:"totalIdr"`
	DisplayName string       `json:"displayName"`
	Tickets     []Ticket     `json:"tickets"`
	ExpiresAt   time.Time    `json:"expiresAt"`
	TicketID    string       `json:"ticketId,omitempty"`
	Transitions []Transition `json:"transitions"`
}
type Ticket struct {
	TicketID string `json:"ticketId"`
	Ordinal  int    `json:"ordinal"`
}

type SectionSales struct {
	Section
	Held       int   `json:"held"`
	Sold       int   `json:"sold"`
	RevenueIDR int64 `json:"revenueIdr"`
}
type Order struct {
	HoldID      string     `json:"holdId"`
	DisplayName string     `json:"displayName"`
	SectionName string     `json:"sectionName"`
	Quantity    int        `json:"quantity"`
	TotalIDR    int64      `json:"totalIdr"`
	Status      HoldStatus `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	ExpiresAt   time.Time  `json:"expiresAt"`
}
type Overview struct {
	EventID      string         `json:"eventId"`
	EventName    string         `json:"eventName"`
	Sections     []SectionSales `json:"sections"`
	RecentOrders []Order        `json:"recentOrders"`
}

var ErrCapacityBelowAllocated = errors.New("capacity below allocated tickets")

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
	AdminOverview(ctx context.Context, session Session) (Overview, error)
	BuyerOrders(ctx context.Context, session Session) ([]Order, error)
	SetCapacity(ctx context.Context, session Session, sectionID string, capacity int) (Section, error)
}
