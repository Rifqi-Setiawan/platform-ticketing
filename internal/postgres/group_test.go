package postgres_test

import (
	"testing"
	"tribunpass/internal/booking"
)

func TestExpireGroupAfterRestartReturnsAllTicketsOnce(t *testing.T) {
	d := setup(t)
	cmd := d.command("group")
	cmd.Quantity = 4
	h, err := d.first.CreateHold(d.ctx, cmd)
	if err != nil {
		t.Fatal(err)
	}
	d.assertInventory(t, d.section.ID, 6, 4, 0, 0)
	if _, err = d.db.ExecContext(d.ctx, `UPDATE holds SET created_at=clock_timestamp()-interval '6 minutes',expires_at=clock_timestamp()-interval '1 minute' WHERE id=$1`, h.HoldID); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.second.ExpireNext(d.ctx); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	e := payment(h, "late")
	e.AmountIDR = h.TotalIDR
	r, err := d.first.ApplyPayment(d.ctx, e, "callback")
	if err != nil || r.Outcome != "RECORDED_LATE" {
		t.Fatal(r, err)
	}
	d.assertInventory(t, d.section.ID, 10, 0, 0, 0)
}

func TestLegacyIdempotencyResultStillReplaysAsOneTicket(t *testing.T) {
	d := setup(t)
	h := d.hold(t)
	_, err := d.db.ExecContext(d.ctx, `UPDATE idempotency_keys SET result = result - 'quantity' - 'totalIdr' - 'displayName' WHERE buyer_id=$1 AND key='first'`, d.session.BuyerID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.second.CreateHold(d.ctx, d.command("first"))
	if err != nil || r != h || r.Quantity != 1 {
		t.Fatal(r, err)
	}
}

func TestGroupRequestsCannotOversell(t *testing.T) {
	d := setup(t)
	var commands []booking.CreateHoldCommand
	for _, key := range []string{"one", "two", "three", "four"} {
		c := d.command(key)
		c.Quantity = 4
		commands = append(commands, c)
	}
	success := 0
	for _, v := range d.contend(commands) {
		if v.err != nil {
			t.Fatal(v.err)
		}
		switch v.hold.Code {
		case "CREATED":
			success++
		case "SOLD_OUT":
		default:
			t.Fatal(v.hold)
		}
	}
	if success != 2 {
		t.Fatal(success)
	}
	d.assertInventory(t, d.section.ID, 2, 8, 0, 0)
}
