package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tribunpass/internal/booking"
)

func recordCreated(ctx context.Context, tx *sql.Tx, holdID, requestID string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO hold_transitions(hold_id,sequence,to_status,cause,occurred_at,request_id)
        SELECT id,1,'HELD','reservation',created_at,$2 FROM holds WHERE id=$1`, holdID, requestID)
	return err
}

func (s *Store) CreateHold(ctx context.Context, cmd booking.CreateHoldCommand) (booking.CreateHoldResult, error) {
	var result booking.CreateHoldResult
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var valid bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS (
        SELECT FROM buyers b JOIN events e ON e.session_id=b.session_id JOIN sections t ON t.event_id=e.id
        WHERE b.id=$1 AND e.session_id=$2 AND t.id=$3)`, cmd.BuyerID, cmd.SessionID, cmd.SectionID).Scan(&valid)
	if err != nil {
		return result, err
	}
	if !valid {
		return result, sql.ErrNoRows
	}

	claim, err := tx.ExecContext(ctx, `INSERT INTO idempotency_keys(buyer_id,operation,key,section_id)
        VALUES ($1,'hold',$2,$3) ON CONFLICT DO NOTHING`, cmd.BuyerID, cmd.Key, cmd.SectionID)
	if err != nil {
		return result, err
	}
	affected, err := claim.RowsAffected()
	if err != nil {
		return result, err
	}
	if affected == 0 {
		// This separate statement sees the winner's commit under READ COMMITTED,
		// even when ON CONFLICT waited for an initially invisible row.
		var sameInput bool
		var status int
		var saved []byte
		err = tx.QueryRowContext(ctx, `SELECT section_id=$3::uuid,http_status,result FROM idempotency_keys
            WHERE buyer_id=$1 AND operation='hold' AND key=$2`, cmd.BuyerID, cmd.Key, cmd.SectionID).Scan(&sameInput, &status, &saved)
		if err != nil {
			return result, err
		}
		if !sameInput {
			return booking.CreateHoldResult{Code: "IDEMPOTENCY_CONFLICT"}, nil
		}
		if err = json.Unmarshal(saved, &result); err != nil {
			return result, err
		}
		result.Code = "CREATED"
		if status == 409 {
			result.Code = "SOLD_OUT"
		}
		return result, nil
	}

	err = tx.QueryRowContext(ctx, `UPDATE sections SET available=available-1
        WHERE id=$1 AND available>0 RETURNING price_idr`, cmd.SectionID).Scan(&result.PriceIDR)
	status := 201
	switch {
	case errors.Is(err, sql.ErrNoRows):
		result.Code = "SOLD_OUT"
		status = 409
	case err != nil:
		return result, err
	default:
		err = tx.QueryRowContext(ctx, `INSERT INTO holds (buyer_id,section_id,price_idr,status,created_at,expires_at)
            SELECT $1,$2,$3,'HELD',t,t+interval '5 minutes' FROM (SELECT clock_timestamp() AS t) clock
            RETURNING id,expires_at`, cmd.BuyerID, cmd.SectionID, result.PriceIDR).Scan(&result.HoldID, &result.ExpiresAt)
		if err != nil {
			return result, err
		}
		result.ExpiresAt = result.ExpiresAt.UTC()
		result.Status = booking.Held
		result.Code = "CREATED"
		if err = recordCreated(ctx, tx, result.HoldID, cmd.RequestID); err != nil {
			return result, err
		}
	}
	saved, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE idempotency_keys SET http_status=$3,result=$4::jsonb
        WHERE buyer_id=$1 AND operation='hold' AND key=$2`, cmd.BuyerID, cmd.Key, status, string(saved))
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}

// One SELECT gives the response a single snapshot of status, ticket and history.
func (s *Store) GetHold(ctx context.Context, sessionID, buyerID, holdID string) (booking.Hold, error) {
	var hold booking.Hold
	rows, err := s.db.QueryContext(ctx, `SELECT h.id,h.section_id,h.status,h.price_idr,h.expires_at,COALESCE(t.id::text,''),
        ht.sequence,COALESCE(ht.from_status,''),ht.to_status,ht.cause,ht.occurred_at
        FROM holds h JOIN buyers b ON b.id=h.buyer_id
        JOIN hold_transitions ht ON ht.hold_id=h.id LEFT JOIN tickets t ON t.hold_id=h.id
        WHERE h.id=$1 AND b.id=$2 AND b.session_id=$3 ORDER BY ht.sequence`, holdID, buyerID, sessionID)
	if err != nil {
		return hold, err
	}
	defer rows.Close()
	for rows.Next() {
		var tr booking.Transition
		if err = rows.Scan(&hold.HoldID, &hold.SectionID, &hold.Status, &hold.PriceIDR, &hold.ExpiresAt, &hold.TicketID,
			&tr.Sequence, &tr.From, &tr.To, &tr.Cause, &tr.At); err != nil {
			return hold, err
		}
		tr.At = tr.At.UTC()
		hold.Transitions = append(hold.Transitions, tr)
	}
	if err = rows.Err(); err != nil {
		return hold, err
	}
	if hold.HoldID == "" {
		return hold, sql.ErrNoRows
	}
	hold.ExpiresAt = hold.ExpiresAt.UTC()
	return hold, nil
}
