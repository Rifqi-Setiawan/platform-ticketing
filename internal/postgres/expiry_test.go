package postgres_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"tribunpass/internal/booking"
)

func TestPaymentWaiterCrossesDeadlineWhileBlocked(t *testing.T) {
	d := setup(t)
	h := d.hold(t)
	locker, err := d.db.BeginTx(d.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback()
	if _, err = locker.ExecContext(d.ctx, "SELECT id FROM holds WHERE id=$1 FOR UPDATE", h.HoldID); err != nil {
		t.Fatal(err)
	}
	skipped, err := d.second.ExpireNext(d.ctx)
	if err != nil || skipped {
		t.Fatalf("worker picked held/live row: %v %v", skipped, err)
	}

	type response struct {
		r   booking.PaymentResult
		err error
	}
	done := make(chan response, 1)
	go func() {
		r, err := d.second.ApplyPayment(d.ctx, payment(h, "waiting"), "waiter")
		done <- response{r, err}
	}()
	// Wait for a PostgreSQL-observed lock dependency, not a scheduler sleep.
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for waiting := false; !waiting; {
		select {
		case early := <-done:
			t.Fatalf("callback completed before lock release: %+v", early)
		case <-timeout.C:
			t.Fatal("payment did not reach the locked hold")
		case <-poll.C:
			err = d.db.QueryRowContext(d.ctx, `SELECT EXISTS(SELECT FROM pg_stat_activity
                WHERE application_name=$1 AND wait_event_type='Lock' AND query LIKE '%FROM holds%FOR UPDATE%')`, d.schema).Scan(&waiting)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	// The payment transaction began before this timestamp. CURRENT_TIMESTAMP
	// would incorrectly confirm; the post-lock wall clock must reject it.
	if _, err = locker.ExecContext(d.ctx, "UPDATE holds SET expires_at=clock_timestamp() WHERE id=$1", h.HoldID); err != nil {
		t.Fatal(err)
	}
	if err = locker.Commit(); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if got.err != nil || got.r.Outcome != "RECORDED_LATE" {
		t.Fatalf("late callback %+v", got)
	}
	d.assertInventory(t, d.section.ID, 10, 0, 0, 0)
	row, err := d.first.GetHold(d.ctx, d.session.ID, d.session.BuyerID, h.HoldID)
	if err != nil || row.Status != booking.Expired || len(row.Transitions) != 2 {
		t.Fatalf("%+v %v", row, err)
	}
}

func TestWorkerRecoveryAndStickySoldOut(t *testing.T) {
	d := setup(t)
	f, err := d.first.CreateFixture(d.ctx, d.session, "late-payment")
	if err != nil {
		t.Fatal(err)
	}
	cmd := d.command("attempt")
	cmd.SectionID = f.SectionID
	r, err := d.first.CreateHold(d.ctx, cmd)
	if err != nil || r.Code != "SOLD_OUT" {
		t.Fatalf("%+v %v", r, err)
	}
	stats, err := d.second.Stats(d.ctx)
	if err != nil || stats.PendingExpired != 1 || stats.OldestOverdueSeconds <= 0 {
		t.Fatalf("%+v %v", stats, err)
	}
	// A fresh instance takes over with nothing from the original process.
	expired, err := d.second.ExpireNext(d.ctx)
	if err != nil || !expired {
		t.Fatalf("worker %v %v", expired, err)
	}
	expired, err = d.second.ExpireNext(d.ctx)
	if err != nil || expired {
		t.Fatalf("duplicate expiry %v %v", expired, err)
	}
	e := booking.PaymentEvent{EventID: "late", HoldID: f.HoldID, AmountIDR: 75000, Type: "payment.succeeded"}
	late, err := d.first.ApplyPayment(d.ctx, e, "after-worker")
	if err != nil || late.Outcome != "RECORDED_LATE" {
		t.Fatalf("%+v %v", late, err)
	}
	r, err = d.second.CreateHold(d.ctx, cmd)
	if err != nil || r.Code != "SOLD_OUT" {
		t.Fatalf("expected sticky sold-out response %+v %v", r, err)
	}
	d.assertInventory(t, f.SectionID, 1, 0, 0, 0)
	cmd.Key = "fresh-attempt"
	r, err = d.second.CreateHold(d.ctx, cmd)
	if err != nil || r.Code != "CREATED" {
		t.Fatalf("expected fresh hold %+v %v", r, err)
	}
	d.assertInventory(t, f.SectionID, 0, 1, 0, 0)
}

func TestExpiryRacingWithCallbackRestocksOnce(t *testing.T) {
	d := setup(t)
	f, err := d.first.CreateFixture(d.ctx, d.session, "late-payment")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	workerDone := make(chan error, 1)
	go func() { <-start; _, err := d.second.ExpireNext(d.ctx); workerDone <- err }()
	close(start)
	e := booking.PaymentEvent{EventID: "late", HoldID: f.HoldID, AmountIDR: 75000, Type: "payment.succeeded"}
	r, err := d.first.ApplyPayment(d.ctx, e, "callback")
	if err != nil || r.Outcome != "RECORDED_LATE" {
		t.Fatalf("%+v %v", r, err)
	}
	if err = <-workerDone; err != nil {
		t.Fatal(err)
	}
	d.assertInventory(t, f.SectionID, 1, 0, 0, 0)
	got, err := d.first.GetHold(d.ctx, d.session.ID, f.BuyerA, f.HoldID)
	if err != nil || got.Status != booking.Expired || len(got.Transitions) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestPurgeExpiredSessionCascadesToPaymentHistory(t *testing.T) {
	d := setup(t)
	h := d.hold(t)
	if _, err := d.first.ApplyPayment(d.ctx, payment(h, "delete-test"), "charge"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.ExecContext(d.ctx, "UPDATE demo_sessions SET expires_at=statement_timestamp() WHERE id=$1", d.session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.first.FindSession(d.ctx, "test-token"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired session accepted: %v", err)
	}
	purged, err := d.first.PurgeExpiredSession(d.ctx)
	if err != nil || !purged {
		t.Fatalf("purge %v %v", purged, err)
	}
	var remaining int
	if err = d.db.QueryRowContext(d.ctx, "SELECT count(*) FROM payment_events").Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("orphaned payment event %d: %v", remaining, err)
	}
}
