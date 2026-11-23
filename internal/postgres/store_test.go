package postgres_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"tribunpass/internal/booking"
	"tribunpass/internal/postgres"
	"tribunpass/migrations"
)

type database struct {
	ctx           context.Context
	first, second *postgres.Store
	db            *sql.DB
	session       booking.Session
	section       booking.Section
	vip           booking.Section
	schema        string
}

// Independent pools emulate instances even when the tests run in one process.
// A fresh schema isolates constraints, fixtures, workers and injected faults.
func setup(t *testing.T) database {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	config, err := pgx.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*config)
	t.Cleanup(func() { admin.Close() })
	var suffix [8]byte
	if _, err = rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	schema := "tribun_test_" + hex.EncodeToString(suffix[:])
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	config.RuntimeParams["search_path"] = schema
	config.RuntimeParams["application_name"] = schema
	firstDB := stdlib.OpenDB(*config)
	secondDB := stdlib.OpenDB(*config)
	firstDB.SetMaxOpenConns(8)
	secondDB.SetMaxOpenConns(8)
	t.Cleanup(func() { firstDB.Close(); secondDB.Close() })
	if err = migrations.Apply(ctx, firstDB); err != nil {
		t.Fatal(err)
	}
	if err = migrations.Apply(ctx, secondDB); err != nil {
		t.Fatal(err)
	}
	first := postgres.New(firstDB)
	session, err := first.CreateSession(ctx, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	events, err := first.Events(ctx, session.ID)
	if err != nil || len(events) != 1 || len(events[0].Sections) != 2 {
		t.Fatalf("seed events: %v %v", events, err)
	}
	return database{ctx, first, postgres.New(secondDB), firstDB, session, events[0].Sections[0], events[0].Sections[1], schema}
}

func (d database) command(key string) booking.CreateHoldCommand {
	return booking.CreateHoldCommand{SessionID: d.session.ID, BuyerID: d.session.BuyerID, SectionID: d.section.ID, Key: key, RequestID: "req-" + key}
}

func (d database) hold(t *testing.T) booking.CreateHoldResult {
	t.Helper()
	h, err := d.first.CreateHold(d.ctx, d.command("first"))
	if err != nil || h.Code != "CREATED" {
		t.Fatalf("create hold: %+v %v", h, err)
	}
	return h
}

func (d database) assertInventory(t *testing.T, sectionID string, wantAvailable, wantHeld, wantConfirmed, wantTickets int) {
	t.Helper()
	var capacity, available, held, confirmed, tickets int
	err := d.db.QueryRowContext(d.ctx, `SELECT s.capacity,s.available,
        COALESCE(sum(h.quantity) FILTER (WHERE h.status='HELD'),0),COALESCE(sum(h.quantity) FILTER (WHERE h.status='CONFIRMED'),0),
        (SELECT count(*) FROM tickets t JOIN holds hh ON hh.id=t.hold_id WHERE hh.section_id=s.id)
        FROM sections s LEFT JOIN holds h ON h.section_id=s.id
        WHERE s.id=$1 GROUP BY s.id`, sectionID).Scan(&capacity, &available, &held, &confirmed, &tickets)
	if err != nil {
		t.Fatal(err)
	}
	if capacity != available+held+confirmed || available != wantAvailable || held != wantHeld || confirmed != wantConfirmed || tickets != wantTickets {
		t.Fatalf("inventory capacity=%d available=%d held=%d confirmed=%d tickets=%d; wanted %d/%d/%d/%d", capacity, available, held, confirmed, tickets, wantAvailable, wantHeld, wantConfirmed, wantTickets)
	}
}

type response struct {
	hold booking.CreateHoldResult
	err  error
}

func (d database) contend(commands []booking.CreateHoldCommand) []response {
	start := make(chan struct{})
	responses := make(chan response, len(commands))
	for i, cmd := range commands {
		store := d.first
		if i%2 == 1 {
			store = d.second
		}
		go func() {
			<-start
			result, err := store.CreateHold(d.ctx, cmd)
			responses <- response{result, err}
		}()
	}
	close(start)
	all := make([]response, 0, len(commands))
	for range commands {
		all = append(all, <-responses)
	}
	return all
}

func TestOneHundredBuyersShareTenTicketsAcrossTwoPools(t *testing.T) {
	d := setup(t)
	commands := make([]booking.CreateHoldCommand, 0, 100)
	rows, err := d.db.QueryContext(d.ctx,
		"INSERT INTO buyers(session_id) SELECT $1 FROM generate_series(1,100) RETURNING id", d.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		cmd := d.command(fmt.Sprintf("buyer-%d", len(commands)))
		if err := rows.Scan(&cmd.BuyerID); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	codes := map[string]int{}
	for _, res := range d.contend(commands) {
		if res.err != nil {
			t.Fatal(res.err)
		}
		codes[res.hold.Code]++
	}
	if codes["CREATED"] != 10 || codes["SOLD_OUT"] != 90 {
		t.Fatalf("results %v", codes)
	}
	d.assertInventory(t, d.section.ID, 0, 10, 0, 0)
}

func TestConcurrentIdempotencyAndChangedPayload(t *testing.T) {
	d := setup(t)
	cmds := make([]booking.CreateHoldCommand, 20)
	for i := range cmds {
		cmds[i] = d.command("retry")
	}
	var id string
	for _, res := range d.contend(cmds) {
		if res.err != nil || res.hold.Code != "CREATED" {
			t.Fatalf("%+v", res)
		}
		if id == "" {
			id = res.hold.HoldID
		}
		if res.hold.HoldID != id {
			t.Fatalf("ID changed: %s vs %s", id, res.hold.HoldID)
		}
	}
	conflict := d.command("retry")
	conflict.SectionID = d.vip.ID
	res, err := d.second.CreateHold(d.ctx, conflict)
	if err != nil || res.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("%+v %v", res, err)
	}
	d.assertInventory(t, d.section.ID, 9, 1, 0, 0)
	d.assertInventory(t, d.vip.ID, 4, 0, 0, 0)
}

func payment(h booking.CreateHoldResult, id string) booking.PaymentEvent {
	return booking.PaymentEvent{EventID: id, HoldID: h.HoldID, AmountIDR: h.PriceIDR, Type: "payment.succeeded"}
}

func TestCallbacksAndReplayAfterTerminalStatus(t *testing.T) {
	d := setup(t)
	h := d.hold(t)
	e := payment(h, "payment-1")
	wrong := e
	wrong.AmountIDR++
	r, err := d.first.ApplyPayment(d.ctx, wrong, "amount")
	if err != nil || r.Outcome != "AMOUNT_MISMATCH" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = d.first.ApplyPayment(d.ctx, e, "changed")
	if err != nil || r.Outcome != "EVENT_CONFLICT" {
		t.Fatalf("%+v %v", r, err)
	}
	e.EventID = "payment-2"
	start := make(chan struct{})
	type reply struct {
		r   booking.PaymentResult
		err error
	}
	done := make(chan reply, 2)
	for _, store := range []*postgres.Store{d.first, d.second} {
		go func() { <-start; v, err := store.ApplyPayment(d.ctx, e, "success"); done <- reply{v, err} }()
	}
	close(start)
	first, second := <-done, <-done
	if first.r.Replayed == second.r.Replayed {
		t.Fatalf("exactly one delivery should be a replay: %+v %+v", first, second)
	}
	first.r.Replayed, second.r.Replayed = false, false
	if first.err != nil || second.err != nil || first.r != second.r || first.r.Outcome != "CONFIRMED" || first.r.TicketID == "" {
		t.Fatalf("duplicate callback results %+v %+v", first, second)
	}
	e.EventID = "payment-3"
	r, err = d.second.ApplyPayment(d.ctx, e, "new")
	if err != nil || r.Outcome != "ALREADY_CONFIRMED" || r.TicketID != first.r.TicketID {
		t.Fatalf("%+v %v", r, err)
	}
	replay, err := d.second.CreateHold(d.ctx, d.command("first"))
	if err != nil || replay != h {
		t.Fatalf("hold replay changed: %+v vs %+v; %v", h, replay, err)
	}
	snapshot, err := d.first.GetHold(d.ctx, d.session.ID, d.session.BuyerID, h.HoldID)
	if err != nil || snapshot.Status != booking.Confirmed || len(snapshot.Transitions) != 2 || snapshot.TicketID != r.TicketID {
		t.Fatalf("%+v %v", snapshot, err)
	}
	d.assertInventory(t, d.section.ID, 9, 0, 1, 1)
}

func TestFaultAfterInventoryUpdateRollsBackAndAllowsRetry(t *testing.T) {
	d := setup(t)
	_, err := d.db.ExecContext(d.ctx, `ALTER TABLE holds ADD CONSTRAINT injected_failure CHECK (status<>'HELD')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.first.CreateHold(d.ctx, d.command("first"))
	if err == nil {
		t.Fatal("expected injected SQL failure")
	}
	d.assertInventory(t, d.section.ID, 10, 0, 0, 0)
	if _, err = d.db.ExecContext(d.ctx, "ALTER TABLE holds DROP CONSTRAINT injected_failure"); err != nil {
		t.Fatal(err)
	}
	d.hold(t)
	d.assertInventory(t, d.section.ID, 9, 1, 0, 0)
}

func TestMissingOrForeignResource(t *testing.T) {
	d := setup(t)
	other, err := d.first.CreateSession(d.ctx, "second")
	if err != nil {
		t.Fatal(err)
	}
	h := d.hold(t)
	_, err = d.first.GetHold(d.ctx, other.ID, other.BuyerID, h.HoldID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("foreign hold: %v", err)
	}
	cmd := d.command("foreign")
	cmd.BuyerID = other.BuyerID
	_, err = d.first.CreateHold(d.ctx, cmd)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("foreign buyer: %v", err)
	}
	_, err = d.second.Section(d.ctx, other.ID, d.section.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("foreign section: %v", err)
	}
	var siblingID string
	err = d.db.QueryRowContext(d.ctx,
		"INSERT INTO buyers(session_id,token_hash) VALUES ($1,'sibling') RETURNING id", d.session.ID).Scan(&siblingID)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := d.second.FindSession(d.ctx, "sibling")
	if err != nil || sibling.ID != d.session.ID || sibling.BuyerID != siblingID {
		t.Fatalf("buyer identity %+v %v", sibling, err)
	}
	_, err = d.second.GetHold(d.ctx, sibling.ID, sibling.BuyerID, h.HoldID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("hold visible to another buyer in same match: %v", err)
	}
}

func TestDistinctSuccessEventsArrivingTogetherIssueSingleTicket(t *testing.T) {
	d := setup(t)
	h := d.hold(t)
	type reply struct {
		payment booking.PaymentResult
		err     error
	}
	done := make(chan reply, 2)
	start := make(chan struct{})
	for i, store := range []*postgres.Store{d.first, d.second} {
		go func() {
			<-start
			result, err := store.ApplyPayment(d.ctx, payment(h, fmt.Sprintf("success-%d", i)), "callback")
			done <- reply{result, err}
		}()
	}
	close(start)
	one, two := <-done, <-done
	if one.err != nil || two.err != nil || one.payment.TicketID == "" || one.payment.TicketID != two.payment.TicketID {
		t.Fatalf("concurrent payments %+v %+v", one, two)
	}
	outcomes := map[string]int{}
	outcomes[one.payment.Outcome]++
	outcomes[two.payment.Outcome]++
	if outcomes["CONFIRMED"] != 1 || outcomes["ALREADY_CONFIRMED"] != 1 {
		t.Fatalf("expected one state transition, got %v", outcomes)
	}
	d.assertInventory(t, d.section.ID, 9, 0, 1, 1)
}
