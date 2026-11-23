package booking

import (
	"testing"
	"time"
)

func TestPaymentDeadline(t *testing.T) {
	end := time.Date(2026, 11, 23, 12, 5, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		state  HoldStatus
		offset time.Duration
		want   string
	}{
		{"before", Held, -time.Microsecond, "CONFIRMED"}, {"equal", Held, 0, "RECORDED_LATE"}, {"after", Held, time.Microsecond, "RECORDED_LATE"},
		{"already issued", Confirmed, time.Second, "ALREADY_CONFIRMED"}, {"already released", Expired, -time.Second, "RECORDED_LATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecidePayment(tc.state, end, end.Add(tc.offset)); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
