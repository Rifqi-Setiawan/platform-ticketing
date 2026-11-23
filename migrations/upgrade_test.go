package migrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Replay the upgrade that a running v1 database receives, including its
// existing hold, issued admission ID and saved idempotency response.
func TestUpgradePreservesIssuedTickets(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL for PostgreSQL migration tests")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	admin := stdlib.OpenDB(*cfg)
	defer admin.Close()
	var salt [8]byte
	if _, err = rand.Read(salt[:]); err != nil {
		t.Fatal(err)
	}
	schema := "tribun_upgrade_" + hex.EncodeToString(salt[:])
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	cfg.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	if _, err = db.ExecContext(ctx, `CREATE TABLE schema_migrations(version text PRIMARY KEY, applied_at timestamptz DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_initial.sql", "002_buyer_credentials.sql"} {
		src, err := scripts.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, string(src)); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES($1)", name); err != nil {
			t.Fatal(err)
		}
	}
	var oldID, holdID string
	err = db.QueryRowContext(ctx, `WITH sandbox AS (INSERT INTO demo_sessions DEFAULT VALUES RETURNING id),
	buyer AS (INSERT INTO buyers(session_id,is_primary) SELECT id,true FROM sandbox RETURNING id),
	event AS (INSERT INTO events(session_id,name) SELECT id,'Legacy final' FROM sandbox RETURNING id),
	tier AS (INSERT INTO sections(event_id,name,capacity,available,price_idr) SELECT id,'Reguler',10,9,75000 FROM event RETURNING id),
	hold AS (INSERT INTO holds(buyer_id,section_id,price_idr,status,created_at,expires_at,finalized_at)
		SELECT buyer.id,tier.id,75000,'CONFIRMED',now(),now()+interval '5 minutes',now() FROM buyer,tier RETURNING *),
	sticky AS (INSERT INTO idempotency_keys(buyer_id,operation,key,section_id,http_status,result)
		SELECT buyer_id,'hold','legacy',section_id,201,jsonb_build_object('holdId',id,'priceIdr',75000) FROM hold)
	INSERT INTO tickets(hold_id,issued_at) SELECT id,now() FROM hold RETURNING id,hold_id`).Scan(&oldID, &holdID)
	if err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	var id string
	var qty, ordinal, fingerprintQty int
	err = db.QueryRowContext(ctx, `SELECT t.id,t.ordinal,h.quantity,k.quantity
	FROM tickets t JOIN holds h ON h.id=t.hold_id JOIN idempotency_keys k ON k.buyer_id=h.buyer_id
	WHERE h.id=$1`, holdID).Scan(&id, &ordinal, &qty, &fingerprintQty)
	if err != nil || id != oldID || qty != 1 || ordinal != 1 || fingerprintQty != 1 {
		t.Fatalf("legacy order %s %d %d %d: %v", id, qty, ordinal, fingerprintQty, err)
	}
	if err = Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
}
