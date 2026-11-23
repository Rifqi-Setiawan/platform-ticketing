// Package postgres implements the booking store with explicit SQL transaction boundaries.
package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"tribunpass/internal/booking"
)

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

var _ booking.Store = (*Store)(nil)

func (s *Store) CreateSession(ctx context.Context, tokenHash string) (booking.Session, error) {
	var session booking.Session
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return session, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, "INSERT INTO demo_sessions DEFAULT VALUES RETURNING id").Scan(&session.ID)
	if err != nil {
		return session, err
	}
	err = tx.QueryRowContext(ctx, "INSERT INTO buyers(session_id,is_primary,token_hash) VALUES ($1,true,$2) RETURNING id", session.ID, tokenHash).Scan(&session.BuyerID)
	if err != nil {
		return session, err
	}
	var eventID string
	err = tx.QueryRowContext(ctx, "INSERT INTO events(session_id, name) VALUES ($1, 'Nusantara Cup Final') RETURNING id", session.ID).Scan(&eventID)
	if err != nil {
		return session, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sections(event_id,name,capacity,available,price_idr)
        VALUES ($1,'Reguler',10,10,75000),($1,'VIP',4,4,150000)`, eventID)
	if err != nil {
		return session, err
	}
	return session, tx.Commit()
}

func (s *Store) FindSession(ctx context.Context, tokenHash string) (booking.Session, error) {
	var session booking.Session
	err := s.db.QueryRowContext(ctx, `SELECT ds.id,b.id FROM demo_sessions ds
        JOIN buyers b ON b.session_id=ds.id
        WHERE b.token_hash=$1 AND ds.expires_at>statement_timestamp()`, tokenHash).Scan(&session.ID, &session.BuyerID)
	return session, err
}

func (s *Store) Events(ctx context.Context, sessionID string) ([]booking.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,e.name,e.starts_at,t.id,t.name,t.capacity,t.available,t.price_idr
        FROM events e JOIN sections t ON t.event_id=e.id
        WHERE e.session_id=$1 AND NOT e.is_fixture ORDER BY e.id,t.price_idr,t.id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []booking.Event{}
	for rows.Next() {
		var id, name string
		var startsAt time.Time
		var tier booking.Section
		if err := rows.Scan(&id, &name, &startsAt, &tier.ID, &tier.Name, &tier.Capacity, &tier.Available, &tier.PriceIDR); err != nil {
			return nil, err
		}
		if len(events) == 0 || events[len(events)-1].ID != id {
			events = append(events, booking.Event{ID: id, Name: name, StartsAt: startsAt.UTC()})
		}
		event := &events[len(events)-1]
		event.Sections = append(event.Sections, tier)
	}
	return events, rows.Err()
}

func (s *Store) Section(ctx context.Context, sessionID, sectionID string) (booking.Section, error) {
	var tier booking.Section
	err := s.db.QueryRowContext(ctx, `SELECT t.id,t.name,t.capacity,t.available,t.price_idr
        FROM sections t JOIN events e ON e.id=t.event_id WHERE e.session_id=$1 AND t.id=$2`, sessionID, sectionID).
		Scan(&tier.ID, &tier.Name, &tier.Capacity, &tier.Available, &tier.PriceIDR)
	return tier, err
}

// CreateFixture seeds an independent single-ticket match. The overdue case
// uses a historical clock so a reviewer can exercise expiry immediately.
func (s *Store) CreateFixture(ctx context.Context, session booking.Session, kind string) (booking.Fixture, error) {
	f := booking.Fixture{BuyerA: session.BuyerID}
	if kind != "last-ticket" && kind != "duplicate-payment" && kind != "late-payment" {
		return f, fmt.Errorf("unknown demo scenario %q", kind)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return f, err
	}
	defer tx.Rollback()
	var eventID string
	err = tx.QueryRowContext(ctx, `INSERT INTO events(session_id,name,is_fixture)
        SELECT session_id,$3,true FROM buyers WHERE id=$1 AND session_id=$2 AND is_primary RETURNING id`,
		session.BuyerID, session.ID, kind).Scan(&eventID)
	if err != nil {
		return f, err
	}
	err = tx.QueryRowContext(ctx, "INSERT INTO buyers(session_id) VALUES ($1) RETURNING id", session.ID).Scan(&f.BuyerB)
	if err != nil {
		return f, err
	}
	available := 1
	if kind == "late-payment" {
		available = 0
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO sections(event_id,name,price_idr,capacity,available)
        VALUES ($1,'Reguler',75000,1,$2) RETURNING id`, eventID, available).Scan(&f.SectionID)
	if err != nil {
		return f, err
	}
	if kind == "late-payment" {
		err = tx.QueryRowContext(ctx, `INSERT INTO holds(buyer_id,section_id,price_idr,status,created_at,expires_at)
            VALUES ($1,$2,75000,'HELD',statement_timestamp()-interval '6 minutes',statement_timestamp()-interval '1 minute')
            RETURNING id`, session.BuyerID, f.SectionID).Scan(&f.HoldID)
		if err != nil {
			return f, err
		}
		if err = recordCreated(ctx, tx, f.HoldID, "demo-fixture"); err != nil {
			return f, err
		}
	}
	return f, tx.Commit()
}

type Stats struct {
	DB                   sql.DBStats
	PendingExpired       int64
	OldestOverdueSeconds float64
}

func (s *Store) Stats(ctx context.Context) (Stats, error) {
	stats := Stats{DB: s.db.Stats()}
	err := s.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(EXTRACT(EPOCH FROM (statement_timestamp()-min(expires_at))),0)
        FROM holds WHERE status='HELD' AND expires_at<=statement_timestamp()`).Scan(&stats.PendingExpired, &stats.OldestOverdueSeconds)
	return stats, err
}

func (s *Store) PurgeExpiredSession(ctx context.Context) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM demo_sessions WHERE id=(
        SELECT id FROM demo_sessions WHERE expires_at<=statement_timestamp()
        ORDER BY expires_at,id LIMIT 1 FOR UPDATE SKIP LOCKED)`)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}
