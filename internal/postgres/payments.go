package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"tribunpass/internal/booking"
)

type lockedHold struct {
	id        string
	sectionID string
	status    booking.HoldStatus
	priceIDR  int64
	quantity  int
	expiresAt time.Time
}

// finalize runs only while the caller owns the HELD row lock. The seat count,
// terminal status, ticket and timeline become visible together.
func finalize(ctx context.Context, tx *sql.Tx, h lockedHold, status booking.HoldStatus, at time.Time, cause, requestID string) (string, error) {
	_, err := tx.ExecContext(ctx, `UPDATE holds SET status=$2,finalized_at=$3 WHERE id=$1`, h.id, status, at)
	if err != nil {
		return "", err
	}
	var ticketID string
	switch status {
	case booking.Confirmed:
		err = tx.QueryRowContext(ctx, `WITH issued AS (
            INSERT INTO tickets(hold_id,issued_at,ordinal)
            SELECT $1,$2,n FROM generate_series(1,$3::integer) AS n RETURNING id,ordinal
        ) SELECT id FROM issued WHERE ordinal=1`, h.id, at, h.quantity).Scan(&ticketID)
	case booking.Expired:
		_, err = tx.ExecContext(ctx, "UPDATE sections SET available=available+$2 WHERE id=$1", h.sectionID, h.quantity)
	default:
		return "", fmt.Errorf("invalid terminal status %s", status)
	}
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO hold_transitions(hold_id,sequence,from_status,to_status,cause,occurred_at,request_id)
        VALUES ($1,2,'HELD',$2,$3,$4,$5)`, h.id, status, cause, at, requestID)
	return ticketID, err
}

func (s *Store) ApplyPayment(ctx context.Context, event booking.PaymentEvent, requestID string) (booking.PaymentResult, error) {
	r := booking.PaymentResult{EventID: event.EventID, HoldID: event.HoldID}
	if event.Type != "payment.succeeded" {
		return r, fmt.Errorf("unsupported payment event %q", event.Type)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	claim, err := tx.ExecContext(ctx, `INSERT INTO payment_events(provider,event_id,hold_id,amount_idr)
        VALUES ('simulator',$1,$2,$3) ON CONFLICT DO NOTHING`, event.EventID, event.HoldID, event.AmountIDR)
	if err != nil {
		return r, err
	}
	affected, err := claim.RowsAffected()
	if err != nil {
		return r, err
	}
	if affected == 0 {
		var sameInput bool
		var saved []byte
		err = tx.QueryRowContext(ctx, `SELECT hold_id=$2::uuid AND amount_idr=$3,result FROM payment_events
            WHERE provider='simulator' AND event_id=$1`, event.EventID, event.HoldID, event.AmountIDR).Scan(&sameInput, &saved)
		if err != nil {
			return r, err
		}
		if !sameInput {
			r.Outcome = "EVENT_CONFLICT"
			return r, nil
		}
		err = json.Unmarshal(saved, &r)
		r.Replayed = err == nil
		return r, err
	}

	var h lockedHold
	err = tx.QueryRowContext(ctx, `SELECT id,section_id,status,price_idr,quantity,expires_at FROM holds
        WHERE id=$1 FOR UPDATE`, event.HoldID).Scan(&h.id, &h.sectionID, &h.status, &h.priceIDR, &h.quantity, &h.expiresAt)
	if err != nil {
		return r, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return r, err
	}
	r.Outcome = booking.DecidePayment(h.status, h.expiresAt, now)
	switch {
	case event.AmountIDR != h.priceIDR*int64(h.quantity):
		r.Outcome = "AMOUNT_MISMATCH"
	case r.Outcome == "ALREADY_CONFIRMED":
		err = tx.QueryRowContext(ctx, "SELECT id FROM tickets WHERE hold_id=$1 AND ordinal=1", h.id).Scan(&r.TicketID)
	case h.status == booking.Held:
		target := booking.Confirmed
		if r.Outcome == "RECORDED_LATE" {
			target = booking.Expired
		}
		r.TicketID, err = finalize(ctx, tx, h, target, now, "payment_callback", requestID)
	}
	if err != nil {
		return r, err
	}
	saved, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE payment_events SET result=$2::jsonb WHERE provider='simulator' AND event_id=$1`, event.EventID, string(saved))
	if err != nil {
		return r, err
	}
	return r, tx.Commit()
}

func (s *Store) ExpireNext(ctx context.Context) (bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var h lockedHold
	err = tx.QueryRowContext(ctx, `SELECT id,section_id,status,price_idr,quantity,expires_at FROM holds
        WHERE status='HELD' AND expires_at<=statement_timestamp()
        ORDER BY expires_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&h.id, &h.sectionID, &h.status, &h.priceIDR, &h.quantity, &h.expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return false, err
	}
	if now.Before(h.expiresAt) {
		return false, nil
	}
	if _, err = finalize(ctx, tx, h, booking.Expired, now, "expiry_worker", "worker-"+h.id); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
