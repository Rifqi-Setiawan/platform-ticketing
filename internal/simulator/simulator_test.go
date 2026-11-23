package simulator

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeliverySignsExactBody(t *testing.T) {
	secret := []byte("test-only")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		sig := r.Header.Get(SignatureHeader)
		if !Verify(secret, body, sig) || Verify(secret, append(body, ' '), sig) ||
			Verify(nil, body, sig) || Verify(secret, body, "bad") {
			t.Error("callback signature verification did not protect exact request body")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"outcome":"CONFIRMED"}`))
	}))
	defer s.Close()
	status, body, err := New(s.URL, secret).Deliver(context.Background(), Event{
		EventID: "test-event", HoldID: "test-hold", Type: "payment.succeeded", AmountIDR: 50000,
	})
	if err != nil || status != 200 || string(body) != `{"outcome":"CONFIRMED"}` {
		t.Fatalf("delivery: status=%d body=%q error=%v", status, body, err)
	}
}

func TestDeliveryDoesNotFollowRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("provider followed redirect with signed callback")
	}))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer s.Close()
	status, _, err := New(s.URL, []byte("test")).Deliver(context.Background(), Event{})
	if err != nil || status != http.StatusTemporaryRedirect {
		t.Fatalf("status=%d err=%v", status, err)
	}
}
