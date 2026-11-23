package system_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"tribunpass/internal/booking"
)

func TestGroupCheckoutAndOrganizerInventory(t *testing.T) {
	c := startCluster(t)
	g := c.guest(t)
	order := map[string]any{"sectionId": g.section, "quantity": 3, "displayName": "  Nisa  "}
	hold := parse[booking.CreateHoldResult](t, c.request(t, 0, "/v1/holds", "POST", g.token, "group", order), 201)
	if hold.Quantity != 3 || hold.PriceIDR != 75000 || hold.TotalIDR != 225000 || hold.DisplayName != "Nisa" {
		t.Fatalf("hold %+v", hold)
	}
	order["displayName"] = "Nisa"
	retry := parse[booking.CreateHoldResult](t, c.request(t, 1, "/v1/holds", "POST", g.token, "group", order), 201)
	if retry != hold {
		t.Fatalf("retry %+v", retry)
	}
	for _, change := range []map[string]any{{"sectionId": g.section, "quantity": 2, "displayName": "Nisa"}, {"sectionId": g.section, "quantity": 3, "displayName": "Different"}} {
		if resp := c.request(t, 1, "/v1/holds", "POST", g.token, "group", change); resp.status != 409 {
			t.Fatalf("changed payload HTTP %d", resp.status)
		}
	}
	adjust := "/v1/admin/sections/" + g.section + "/capacity"
	if resp := c.request(t, 1, adjust, "POST", g.token, "", map[string]int{"capacity": 2}); resp.status != 409 {
		t.Fatalf("shrinking below allocated HTTP %d", resp.status)
	}
	s := parse[booking.Section](t, c.request(t, 1, adjust, "POST", g.token, "", map[string]int{"capacity": 6}), 200)
	if s.Available != 3 {
		t.Fatal(s)
	}
	dash := parse[booking.Overview](t, c.request(t, 1, "/v1/admin/overview", "GET", g.token, "", nil), 200)
	if len(dash.RecentOrders) != 1 || dash.RecentOrders[0].DisplayName != "Nisa" || dash.Sections[0].Held != 3 || dash.Sections[0].RevenueIDR != 0 {
		t.Fatalf("dashboard %+v", dash)
	}
	payment := map[string]string{"eventId": "group-payment", "holdId": hold.HoldID}
	receipt := parse[booking.PaymentResult](t, c.request(t, 0, "/v1/demo/payments", "POST", g.token, "", payment), 200)
	if receipt.Outcome != "CONFIRMED" {
		t.Fatal(receipt)
	}
	replay := parse[booking.PaymentResult](t, c.request(t, 1, "/v1/demo/payments", "POST", g.token, "", payment), 200)
	if replay != receipt {
		t.Fatal(replay)
	}
	payment["eventId"] = "different-payment"
	duplicate := parse[booking.PaymentResult](t, c.request(t, 1, "/v1/demo/payments", "POST", g.token, "", payment), 200)
	if duplicate.TicketID != receipt.TicketID || duplicate.Outcome != "ALREADY_CONFIRMED" {
		t.Fatal(duplicate)
	}
	current := parse[booking.Hold](t, c.request(t, 1, "/v1/holds/"+hold.HoldID, "GET", g.token, "", nil), 200)
	if len(current.Tickets) != 3 || len(current.Transitions) != 2 || current.Tickets[0].TicketID != receipt.TicketID {
		t.Fatalf("tickets %+v", current)
	}
	seen := map[string]bool{}
	for i, ticket := range current.Tickets {
		if ticket.Ordinal != i+1 || ticket.TicketID == "" || seen[ticket.TicketID] {
			t.Fatal(ticket)
		}
		seen[ticket.TicketID] = true
	}
	dash = parse[booking.Overview](t, c.request(t, 0, "/v1/admin/overview", "GET", g.token, "", nil), 200)
	sold := dash.Sections[0]
	if sold.Sold != 3 || sold.Held != 0 || sold.Available != 3 || sold.RevenueIDR != 225000 || dash.RecentOrders[0].Status != booking.Confirmed {
		t.Fatal(dash)
	}
	c.inventory(t, g.section, 3, 0, 3)
	wallet := parse[struct{ Orders []booking.Order }](t, c.request(t, 0, "/v1/me/orders", "GET", g.token, "", nil), 200)
	if len(wallet.Orders) != 1 || wallet.Orders[0].HoldID != hold.HoldID || wallet.Orders[0].Status != booking.Confirmed {
		t.Fatal(wallet)
	}
	for _, qty := range []int{0, 5, -1} {
		if resp := c.request(t, 0, "/v1/holds", "POST", g.token, "invalid", map[string]any{"sectionId": g.section, "quantity": qty}); resp.status != 400 {
			t.Fatalf("quantity %d allowed", qty)
		}
	}
	if r := c.request(t, 0, "/v1/holds", "POST", g.token, "too-many", map[string]any{"sectionId": g.section, "quantity": 4}); r.status != 409 {
		t.Fatalf("insufficient stock HTTP %d", r.status)
	}
	c.inventory(t, g.section, 3, 0, 3)
}

func TestOrganizerIsolationAndFixtures(t *testing.T) {
	c := startCluster(t)
	own := c.guest(t)
	foreign := c.guest(t)
	adjust := "/v1/admin/sections/" + own.section + "/capacity"
	if r := c.request(t, 0, adjust, "POST", foreign.token, "", map[string]int{"capacity": 800}); r.status != 404 {
		t.Fatalf("foreign edit %d", r.status)
	}
	secondary := randomHex(32)
	digest := sha256.Sum256([]byte(secondary))
	if _, err := c.db.ExecContext(c.ctx, "INSERT INTO buyers(session_id,token_hash) VALUES ($1,$2)", own.session, hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	if r := c.request(t, 0, adjust, "POST", secondary, "", map[string]int{"capacity": 99}); r.status != 404 {
		t.Fatal(r.status)
	}
	if r := c.request(t, 0, "/v1/admin/overview", "GET", secondary, "", nil); r.status != 404 {
		t.Fatal(r.status)
	}
	if r := c.request(t, 0, "/v1/admin/overview", "GET", "", "", nil); r.status != 401 {
		t.Fatal(r.status)
	}
	scenario := parse[struct {
		SectionID string `json:"sectionId"`
	}](t, c.request(t, 0, "/v1/demo/scenarios", "POST", own.token, "", map[string]string{"scenario": "duplicate-payment"}), 200)
	if r := c.request(t, 0, "/v1/admin/sections/"+scenario.SectionID+"/capacity", "POST", own.token, "", map[string]int{"capacity": 90}); r.status != 404 {
		t.Fatal(r.status)
	}
	dash := parse[booking.Overview](t, c.request(t, 0, "/v1/admin/overview", "GET", own.token, "", nil), 200)
	if len(dash.RecentOrders) != 0 || len(dash.Sections) != 2 || dash.Sections[0].Available != 10 || dash.Sections[0].Sold != 0 {
		t.Fatal(dash)
	}
	created := parse[booking.CreateHoldResult](t, c.request(t, 0, "/v1/holds", "POST", secondary, "sibling", map[string]any{"sectionId": own.section}), 201)
	type wallet struct{ Orders []booking.Order }
	for _, token := range []string{own.token, foreign.token} {
		mine := parse[wallet](t, c.request(t, 0, "/v1/me/orders", "GET", token, "", nil), 200)
		if len(mine.Orders) != 0 {
			t.Fatalf("foreign or fixture order leaked %+v", mine)
		}
	}
	mine := parse[wallet](t, c.request(t, 1, "/v1/me/orders", "GET", secondary, "", nil), 200)
	if len(mine.Orders) != 1 || mine.Orders[0].HoldID != created.HoldID {
		t.Fatal(mine)
	}
}
