package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tribunpass/internal/booking"
)

const (
	testHoldID    = "37c9499b-9e60-411f-9fe6-2bd3c2a7d69a"
	testSectionID = "c9af7844-31d9-4b9b-9c23-50c8edb85591"
)

// The store is stubbed only at the transport boundary; PostgreSQL's invariants
// are exercised independently with real transactions.
type stubStore struct {
	booking.Store
	hash        string
	holdCommand booking.CreateHoldCommand
	holdCode    string
	seen        booking.PaymentEvent
	holdOwner   string
}

func (s *stubStore) FindSession(_ context.Context, hash string) (booking.Session, error) {
	if hash != s.hash {
		return booking.Session{}, sql.ErrNoRows
	}
	return booking.Session{ID: "session", BuyerID: "buyer"}, nil
}
func (s *stubStore) CreateSession(_ context.Context, hash string) (booking.Session, error) {
	s.hash = hash
	return booking.Session{ID: "session", BuyerID: "buyer"}, nil
}
func (s *stubStore) CreateHold(_ context.Context, c booking.CreateHoldCommand) (booking.CreateHoldResult, error) {
	s.holdCommand = c
	return booking.CreateHoldResult{Code: s.holdCode, HoldID: testHoldID, Status: booking.Held}, nil
}
func (s *stubStore) GetHold(_ context.Context, session, buyer, hold string) (booking.Hold, error) {
	if session != "session" || buyer != s.holdOwner || hold != testHoldID {
		return booking.Hold{}, sql.ErrNoRows
	}
	return booking.Hold{HoldID: hold, PriceIDR: 50000, TotalIDR: 50000, Quantity: 1}, nil
}
func (s *stubStore) ApplyPayment(_ context.Context, event booking.PaymentEvent, _ string) (booking.PaymentResult, error) {
	s.seen = event
	return booking.PaymentResult{EventID: event.EventID, HoldID: event.HoldID, Outcome: "CONFIRMED", TicketID: "ticket"}, nil
}

func testHandler(store booking.Store, url string) http.Handler {
	return New(store, Config{CallbackURL: url, Secret: []byte("test-only-secret"), SecureCookies: true,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func post(h http.Handler, path, body string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestSessionStoresOnlyHashAndReusesCookie(t *testing.T) {
	store := new(stubStore)
	h := testHandler(store, "")
	w := post(h, "/v1/demo-sessions", "", nil, nil)
	if w.Code != 201 {
		t.Fatalf("new session: %d %s", w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	c := cookies[0]
	if c.Name != sessionCookie || len(c.Value) != 64 || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode ||
		c.Value == store.hash || tokenHash(c.Value) != store.hash {
		t.Fatalf("unexpected cookie properties or stored token")
	}
	w = post(h, "/v1/demo-sessions", "{}", c, nil)
	if w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("session reuse: %d %v", w.Code, w.Header())
	}
}

func TestHoldRejectsMalformedRequestsAndMapsDurableOutcome(t *testing.T) {
	store := &stubStore{holdCode: "CREATED"}
	h := testHandler(store, "")
	cookie := post(h, "/v1/demo-sessions", "", nil, nil).Result().Cookies()[0]
	key := map[string]string{"Idempotency-Key": "purchase-1"}
	valid := `{"sectionId":"` + testSectionID + `"}`
	for _, tt := range []struct {
		name, body string
		cookie     *http.Cookie
		headers    map[string]string
		status     int
	}{
		{"guest", valid, nil, key, 401},
		{"missing key", valid, cookie, nil, 400},
		{"unknown field", `{"sectionId":"` + testSectionID + `","priceIdr":1}`, cookie, key, 400},
		{"trailing object", valid + `{}`, cookie, key, 400},
		{"cross site", valid, cookie, map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
		{"body limit", strings.Repeat("x", maxBody+1), cookie, key, 413},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := post(h, "/v1/holds", tt.body, tt.cookie, tt.headers)
			if w.Code != tt.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
	w := post(h, "/v1/holds", valid, cookie, key)
	if w.Code != 201 || store.holdCommand.BuyerID != "buyer" || store.holdCommand.SessionID != "session" ||
		store.holdCommand.RequestID != w.Header().Get("X-Request-ID") {
		t.Fatalf("%d command=%+v", w.Code, store.holdCommand)
	}
	for _, code := range []string{"SOLD_OUT", "IDEMPOTENCY_CONFLICT"} {
		store.holdCode = code
		w = post(h, "/v1/holds", valid, cookie, key)
		if w.Code != 409 || !strings.Contains(w.Body.String(), code) {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
}

func TestSimulatorVerifiesOwnershipAndSignsRealCallback(t *testing.T) {
	store := &stubStore{holdOwner: "buyer"}
	callback := httptest.NewServer(testHandler(store, ""))
	defer callback.Close()
	h := testHandler(store, callback.URL+"/v1/payment-callbacks/simulator")
	cookie := post(h, "/v1/demo-sessions", "", nil, nil).Result().Cookies()[0]
	body := `{"holdId":"` + testHoldID + `","eventId":"payment-1"}`
	w := post(h, "/v1/demo/payments", body, cookie, nil)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var result booking.PaymentResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Outcome != "CONFIRMED" ||
		store.seen.AmountIDR != 50000 || store.seen.EventID != "payment-1" {
		t.Fatalf("result=%+v event=%+v", result, store.seen)
	}
	store.holdOwner = "other"
	w = post(h, "/v1/demo/payments", body, cookie, nil)
	if w.Code != 404 {
		t.Fatalf("foreign hold payment: %d", w.Code)
	}
	store.seen = booking.PaymentEvent{}
	w = post(h, "/v1/payment-callbacks/simulator", "malformed json", nil, nil)
	if w.Code != 401 || store.seen.EventID != "" {
		t.Fatal("unsigned callback reached parser/store")
	}
}
