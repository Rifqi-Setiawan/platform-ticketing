// Package system exercises two independently connected Go HTTP servers against
// the same PostgreSQL inventory through the product's public endpoints.
package system_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"tribunpass/internal/booking"
	"tribunpass/internal/httpapi"
	"tribunpass/internal/postgres"
	"tribunpass/migrations"
)

type cluster struct {
	db     *sql.DB
	urls   [2]string
	ctx    context.Context
	client *http.Client
}

func randomHex(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return hex.EncodeToString(raw)
}

func startCluster(t *testing.T) *cluster {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run multi-instance HTTP tests")
	}
	config, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*config)
	t.Cleanup(func() { admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	schema := "tribun_system_" + randomHex(8)
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := admin.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	config.RuntimeParams["search_path"] = schema
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 100}}
	t.Cleanup(client.CloseIdleConnections)
	cluster := &cluster{ctx: ctx, client: client}
	servers := [2]*httptest.Server{httptest.NewUnstartedServer(nil), httptest.NewUnstartedServer(nil)}
	for i, srv := range servers {
		cluster.urls[i] = "http://" + srv.Listener.Addr().String()
	}
	for i, srv := range servers {
		pool := stdlib.OpenDB(*config)
		pool.SetMaxOpenConns(10)
		t.Cleanup(func() { pool.Close() })
		if i == 0 {
			cluster.db = pool
		}
		if err := migrations.Apply(ctx, pool); err != nil {
			t.Fatal(err)
		}
		// Deliver to the peer so payment initiation and acknowledgement cross
		// both the HTTP boundary and a database connection pool boundary.
		srv.Config.Handler = httpapi.New(postgres.New(pool), httpapi.Config{
			CallbackURL: cluster.urls[1-i] + "/v1/payment-callbacks/simulator",
			Secret:      []byte("shared-system-test-secret"), Ready: pool.PingContext,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		srv.Start()
		t.Cleanup(srv.Close)
	}
	return cluster
}

type response struct {
	status  int
	body    []byte
	cookies []*http.Cookie
}

func send(ctx context.Context, client *http.Client, url, method, token, key string, payload any) (response, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return response{}, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return response{}, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "tribunpass_session", Value: token})
	}
	res, err := client.Do(req)
	if err != nil {
		return response{}, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	return response{res.StatusCode, data, res.Cookies()}, err
}

func parse[T any](t *testing.T, r response, wantStatus int) T {
	t.Helper()
	if r.status != wantStatus {
		t.Fatalf("HTTP %d, want %d: %s", r.status, wantStatus, r.body)
	}
	var value T
	if err := json.Unmarshal(r.body, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func (c *cluster) request(t *testing.T, instance int, path, method, token, key string, payload any) response {
	t.Helper()
	r, err := send(c.ctx, c.client, c.urls[instance]+path, method, token, key, payload)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type guest struct{ session, token, section string }

func (c *cluster) guest(t *testing.T) guest {
	t.Helper()
	resp := c.request(t, 0, "/v1/demo-sessions", "POST", "", "", nil)
	opened := parse[struct {
		SessionID string `json:"sessionId"`
	}](t, resp, 201)
	if len(resp.cookies) != 1 || resp.cookies[0].Name != "tribunpass_session" || opened.SessionID == "" {
		t.Fatal("missing session identity")
	}
	token := resp.cookies[0].Value
	events := parse[struct {
		Events []booking.Event `json:"events"`
	}](t, c.request(t, 1, "/v1/events", "GET", token, "", nil), 200)
	for _, e := range events.Events {
		for _, s := range e.Sections {
			if s.Name == "Reguler" && s.Capacity == 10 {
				return guest{opened.SessionID, token, s.ID}
			}
		}
	}
	t.Fatal("missing ten-ticket Reguler section")
	return guest{}
}

func (c *cluster) inventory(t *testing.T, section string, available, held, confirmed int) {
	t.Helper()
	var cap, remaining, active, paid, tickets int
	err := c.db.QueryRowContext(c.ctx, `SELECT s.capacity,s.available,
		count(h.id) FILTER (WHERE h.status='HELD'),count(h.id) FILTER (WHERE h.status='CONFIRMED'),count(t.id)
		FROM sections s LEFT JOIN holds h ON s.id=h.section_id LEFT JOIN tickets t ON t.hold_id=h.id
		WHERE s.id=$1 GROUP BY s.id`, section).Scan(&cap, &remaining, &active, &paid, &tickets)
	if err != nil {
		t.Fatal(err)
	}
	if remaining != available || active != held || paid != confirmed || cap != remaining+active+paid || tickets != paid {
		t.Fatalf("inventory is inconsistent: capacity=%d remaining=%d held=%d confirmed=%d tickets=%d", cap, remaining, active, paid, tickets)
	}
}

func TestTwoAPIsSellTenTicketsToOneHundredDistinctBuyers(t *testing.T) {
	c := startCluster(t)
	g := c.guest(t)
	tokens := make([]string, 100)
	tokens[0] = g.token
	hashes := make([]string, 0, len(tokens)-1)
	for i := 1; i < len(tokens); i++ {
		tokens[i] = randomHex(32)
		digest := sha256.Sum256([]byte(tokens[i]))
		hashes = append(hashes, hex.EncodeToString(digest[:]))
	}
	if _, err := c.db.ExecContext(c.ctx, "INSERT INTO buyers(session_id,token_hash) SELECT $1,unnest($2::text[])", g.session, hashes); err != nil {
		t.Fatal(err)
	}
	type attempt struct {
		resp response
		err  error
	}
	start := make(chan struct{})
	done := make(chan attempt, len(tokens))
	for i, token := range tokens {
		go func() {
			<-start
			// Each buyer uses the same key text, proving its scope is per buyer.
			resp, err := send(c.ctx, c.client, c.urls[i%2]+"/v1/holds", "POST", token, "purchase", map[string]string{"sectionId": g.section})
			done <- attempt{resp, err}
		}()
	}
	close(start)
	created := map[string]bool{}
	soldOut := 0
	for range tokens {
		a := <-done
		if a.err != nil {
			t.Fatal(a.err)
		}
		switch a.resp.status {
		case 201:
			h := parse[booking.CreateHoldResult](t, a.resp, 201)
			if h.HoldID == "" || created[h.HoldID] {
				t.Fatalf("non-unique hold %q", h.HoldID)
			}
			created[h.HoldID] = true
		case 409:
			e := parse[struct{ Error struct{ Code string } }](t, a.resp, 409)
			if e.Error.Code != "SOLD_OUT" {
				t.Fatalf("unexpected conflict: %s", a.resp.body)
			}
			soldOut++
		default:
			t.Fatalf("unexpected HTTP %d: %s", a.resp.status, a.resp.body)
		}
	}
	if len(created) != 10 || soldOut != 90 {
		t.Fatalf("created=%d sold out=%d", len(created), soldOut)
	}
	c.inventory(t, g.section, 0, 10, 0)
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDropped201ReplayPaymentAndSessionIsolation(t *testing.T) {
	c := startCluster(t)
	g := c.guest(t)
	input := map[string]string{"sectionId": g.section}
	lost := errors.New("simulated response loss")
	var original booking.CreateHoldResult
	// Read and discard an actual HTTP 201 only after the handler has completed
	// its database commit. The caller receives a transport error, never the ID.
	drop := &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		resp, err := c.client.Transport.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 {
			return nil, fmt.Errorf("expected original 201, got %d", resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(&original); err != nil {
			return nil, err
		}
		return nil, lost
	})}
	_, err := send(c.ctx, drop, c.urls[0]+"/v1/holds", "POST", g.token, "purchase-1", input)
	if !errors.Is(err, lost) || original.HoldID == "" {
		t.Fatalf("fault was not injected after the 201: %v", err)
	}
	replay := parse[booking.CreateHoldResult](t, c.request(t, 1, "/v1/holds", "POST", g.token, "purchase-1", input), 201)
	if replay != original {
		t.Fatalf("different replay: %+v / %+v", original, replay)
	}
	c.inventory(t, g.section, 9, 1, 0)

	foreign := c.guest(t)
	for _, r := range []response{
		c.request(t, 1, "/v1/holds/"+replay.HoldID, "GET", foreign.token, "", nil),
		c.request(t, 1, "/v1/demo/payments", "POST", foreign.token, "", map[string]string{"holdId": replay.HoldID, "eventId": "foreign"}),
		c.request(t, 1, "/v1/holds", "POST", foreign.token, "foreign-hold", input),
	} {
		if r.status != 404 {
			t.Fatalf("foreign operation returned HTTP %d: %s", r.status, r.body)
		}
	}
	c.inventory(t, g.section, 9, 1, 0)
	payment := map[string]string{"holdId": replay.HoldID, "eventId": "pay-1"}
	p1 := parse[booking.PaymentResult](t, c.request(t, 0, "/v1/demo/payments", "POST", g.token, "", payment), 200)
	p2 := parse[booking.PaymentResult](t, c.request(t, 1, "/v1/demo/payments", "POST", g.token, "", payment), 200)
	if p1 != p2 || p1.Outcome != "CONFIRMED" || p1.TicketID == "" {
		t.Fatalf("replayed receipt differs: %+v / %+v", p1, p2)
	}
	current := parse[booking.Hold](t, c.request(t, 0, "/v1/holds/"+replay.HoldID, "GET", g.token, "", nil), 200)
	if current.TicketID != p1.TicketID || current.Status != booking.Confirmed || len(current.Transitions) != 2 {
		t.Fatalf("current hold: %+v", current)
	}
	c.inventory(t, g.section, 9, 0, 1)
}
