// Package simulator implements the demo payment provider's HTTP delivery contract.
package simulator

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"tribunpass/internal/booking"
)

const SignatureHeader = "X-TribunPass-Signature"

type Event = booking.PaymentEvent

func signature(secret, raw []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(raw)
	return mac.Sum(nil)
}

// Verify authenticates the original bytes, including whitespace and field order.
func Verify(secret, raw []byte, header string) bool {
	digest, ok := strings.CutPrefix(header, "sha256=")
	if !ok || len(secret) == 0 {
		return false
	}
	got, err := hex.DecodeString(digest)
	return err == nil && hmac.Equal(got, signature(secret, raw))
}

type Client struct {
	url    string
	secret []byte
	http   *http.Client
}

func New(url string, secret []byte) *Client {
	return &Client{
		url: url, secret: secret,
		http: &http.Client{
			Timeout: 4 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// Deliver sends one delivery attempt. A caller may retry using the same EventID
// after a network failure even when the original database commit succeeded.
func (c *Client) Deliver(ctx context.Context, event Event) (int, []byte, error) {
	if len(c.secret) == 0 {
		return 0, nil, errors.New("callback signing key is missing")
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SignatureHeader, "sha256="+hex.EncodeToString(signature(c.secret, raw)))
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("deliver payment callback: %w", err)
	}
	defer resp.Body.Close()
	const limit = 64 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(body) > limit {
		return 0, nil, errors.New("invalid callback response body")
	}
	return resp.StatusCode, body, nil
}
