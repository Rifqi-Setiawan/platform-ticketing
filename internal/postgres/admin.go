package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"tribunpass/internal/booking"
)

// The section counts and order list share one database snapshot. The primary
// buyer operates only their own demo event; review-lab fixtures are excluded.
func (s *Store) AdminOverview(ctx context.Context, session booking.Session) (booking.Overview, error) {
	o := booking.Overview{Sections: []booking.SectionSales{}, RecentOrders: []booking.Order{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return o, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT e.id,e.name FROM events e JOIN buyers b ON b.session_id=e.session_id
		WHERE e.session_id=$1 AND b.id=$2 AND b.is_primary AND NOT e.is_fixture`, session.ID, session.BuyerID).Scan(&o.EventID, &o.EventName)
	if err != nil {
		return o, err
	}
	sections, err := tx.QueryContext(ctx, `SELECT s.id,s.name,s.capacity,s.available,s.price_idr,
		COALESCE(sum(h.quantity) FILTER (WHERE h.status='HELD'),0),
		COALESCE(sum(h.quantity) FILTER (WHERE h.status='CONFIRMED'),0),
		COALESCE(sum(h.quantity*h.price_idr) FILTER (WHERE h.status='CONFIRMED'),0)
		FROM sections s LEFT JOIN holds h ON h.section_id=s.id
		WHERE s.event_id=$1 GROUP BY s.id ORDER BY s.price_idr,s.id`, o.EventID)
	if err != nil {
		return o, err
	}
	defer sections.Close()
	for sections.Next() {
		var a booking.SectionSales
		if err = sections.Scan(&a.ID, &a.Name, &a.Capacity, &a.Available, &a.PriceIDR, &a.Held, &a.Sold, &a.RevenueIDR); err != nil {
			return o, err
		}
		o.Sections = append(o.Sections, a)
	}
	if err = sections.Err(); err != nil {
		return o, err
	}
	o.RecentOrders, err = recentOrders(ctx, tx, session.ID, &o.EventID, nil)
	if err != nil {
		return o, err
	}
	return o, tx.Commit()
}

func (s *Store) BuyerOrders(ctx context.Context, session booking.Session) ([]booking.Order, error) {
	return recentOrders(ctx, s.db, session.ID, nil, &session.BuyerID)
}

type orderReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func recentOrders(ctx context.Context, reader orderReader, sessionID string, eventID, buyerID *string) ([]booking.Order, error) {
	result := []booking.Order{}
	orders, err := reader.QueryContext(ctx, `SELECT h.id,h.display_name,s.name,h.quantity,h.quantity*h.price_idr,h.status,h.created_at,h.expires_at
		FROM holds h JOIN sections s ON s.id=h.section_id JOIN events e ON e.id=s.event_id
		WHERE e.session_id=$1 AND NOT e.is_fixture AND ($2::uuid IS NULL OR e.id=$2) AND ($3::uuid IS NULL OR h.buyer_id=$3)
		ORDER BY h.created_at DESC,h.id DESC LIMIT 50`, sessionID, eventID, buyerID)
	if err != nil {
		return nil, err
	}
	defer orders.Close()
	for orders.Next() {
		var a booking.Order
		if err = orders.Scan(&a.HoldID, &a.DisplayName, &a.SectionName, &a.Quantity, &a.TotalIDR, &a.Status, &a.CreatedAt, &a.ExpiresAt); err != nil {
			return nil, err
		}
		a.CreatedAt = a.CreatedAt.UTC()
		a.ExpiresAt = a.ExpiresAt.UTC()
		result = append(result, a)
	}
	return result, orders.Err()
}

func (s *Store) SetCapacity(ctx context.Context, session booking.Session, sectionID string, capacity int) (booking.Section, error) {
	var result booking.Section
	if capacity < 1 || capacity > 1000 {
		return result, fmt.Errorf("capacity must be between 1 and 1000")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT s.id,s.name,s.capacity,s.available,s.price_idr FROM sections s
		JOIN events e ON e.id=s.event_id JOIN buyers b ON b.session_id=e.session_id
		WHERE s.id=$1 AND e.session_id=$2 AND b.id=$3 AND b.is_primary AND NOT e.is_fixture FOR UPDATE OF s`,
		sectionID, session.ID, session.BuyerID).Scan(&result.ID, &result.Name, &result.Capacity, &result.Available, &result.PriceIDR)
	if err != nil {
		return result, err
	}
	allocated := result.Capacity - result.Available
	if capacity < allocated {
		return result, booking.ErrCapacityBelowAllocated
	}
	result.Capacity = capacity
	result.Available = capacity - allocated
	_, err = tx.ExecContext(ctx, `UPDATE sections SET capacity=$2,available=$3 WHERE id=$1`, sectionID, result.Capacity, result.Available)
	if err != nil {
		return result, err
	}
	return result, tx.Commit()
}
