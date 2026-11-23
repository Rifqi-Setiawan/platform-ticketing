// Package httpapi exposes the TribunPass reservation and simulator contracts.
package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"time"

	"tribunpass/internal/booking"
	"tribunpass/internal/simulator"
)

const (
	sessionCookie  = "tribunpass_session"
	sessionSeconds = 24 * 60 * 60
	maxBody        = 8 << 10
)

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	keyPattern  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
)

type Config struct {
	CallbackURL   string
	Secret        []byte
	SecureCookies bool
	Logger        *slog.Logger
	Ready         func(context.Context) error
}

type server struct {
	store     booking.Store
	config    Config
	simulator *simulator.Client
}

type sessionHandler func(http.ResponseWriter, *http.Request, booking.Session)

func New(store booking.Store, cfg Config) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	s := &server{store: store, config: cfg, simulator: simulator.New(cfg.CallbackURL, cfg.Secret)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("POST /v1/demo-sessions", s.createSession)
	mux.HandleFunc("GET /v1/events", s.withSession(s.events))
	mux.HandleFunc("POST /v1/holds", s.withSession(s.createHold))
	mux.HandleFunc("GET /v1/holds/{id}", s.withSession(s.getHold))
	mux.HandleFunc("POST /v1/demo/payments", s.withSession(s.pay))
	mux.HandleFunc("POST /v1/demo/scenarios", s.withSession(s.scenario))
	mux.HandleFunc("POST /v1/payment-callbacks/simulator", s.callback)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := randomID()
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), requestIDKey{}, id), 10*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		rec := &recorder{ResponseWriter: w, status: 200}
		defer func() {
			cfg.Logger.Info("http request", "request_id", id, "method", r.Method, "path", r.URL.Path,
				"status", rec.status, "duration_ms", time.Since(start).Milliseconds())
		}()
		if r.Method == http.MethodPost && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			fail(rec, r, 403, "CROSS_SITE_REQUEST", "Cross-site writes are blocked.")
			return
		}
		_, pattern := mux.Handler(r)
		if pattern == "" {
			// ServeMux generates plain-text method errors. Keep API failures JSON.
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				probe := r.Clone(ctx)
				probe.Method = method
				if _, matched := mux.Handler(probe); matched != "" {
					rec.Header().Set("Allow", method)
					fail(rec, r, 405, "METHOD_NOT_ALLOWED", "Method not supported.")
					return
				}
			}
			fail(rec, r, 404, "NOT_FOUND", "Resource not found.")
			return
		}
		mux.ServeHTTP(rec, r)
	})
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (w *recorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

type requestIDKey struct{}

func requestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message}, "requestId": requestID(r),
	})
}

func (s *server) storeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, r, 404, "NOT_FOUND", "Resource not found.")
		return
	}
	s.config.Logger.Error("reservation service unavailable", "request_id", requestID(r), "error", err)
	w.Header().Set("Retry-After", "1")
	fail(w, r, 503, "TEMPORARILY_UNAVAILABLE", "Retry using the same request key or payment event ID.")
}

func bodyBytes(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(w, r, 415, "UNSUPPORTED_MEDIA_TYPE", "Set Content-Type to application/json.")
		return nil, false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var max *http.MaxBytesError
		if errors.As(err, &max) {
			fail(w, r, 413, "BODY_TOO_LARGE", "Request body exceeds 8 KiB.")
		} else {
			fail(w, r, 400, "INVALID_REQUEST", "Request body could not be read.")
		}
		return nil, false
	}
	return raw, true
}

func decode(raw []byte, dst any) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(dst) == nil && d.Decode(new(any)) == io.EOF
}

func jsonBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	raw, ok := bodyBytes(w, r)
	if !ok {
		return false
	}
	if !decode(raw, dst) {
		fail(w, r, 400, "INVALID_REQUEST", "Expected one JSON object with the documented fields.")
		return false
	}
	return true
}

func (s *server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if s.config.Ready != nil && s.config.Ready(ctx) != nil {
		fail(w, r, 503, "NOT_READY", "Database unavailable.")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func tokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func (s *server) session(r *http.Request) (booking.Session, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) != 64 {
		return booking.Session{}, sql.ErrNoRows
	}
	if _, err := hex.DecodeString(cookie.Value); err != nil {
		return booking.Session{}, sql.ErrNoRows
	}
	return s.store.FindSession(r.Context(), tokenHash(cookie.Value))
}

func (s *server) withSession(next sessionHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := s.session(r)
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, r, 401, "SESSION_REQUIRED", "Create a demo session first.")
			return
		}
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		next(w, r, session)
	}
}

func (s *server) createSession(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength != 0 {
		var empty struct{}
		if !jsonBody(w, r, &empty) {
			return
		}
	}
	session, err := s.session(r)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.storeError(w, r, err)
		return
	}
	status := 200
	if errors.Is(err, sql.ErrNoRows) {
		var tokenBytes [32]byte
		if _, err := rand.Read(tokenBytes[:]); err != nil {
			s.storeError(w, r, err)
			return
		}
		token := hex.EncodeToString(tokenBytes[:])
		session, err = s.store.CreateSession(r.Context(), tokenHash(token))
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Secure: s.config.SecureCookies, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: sessionSeconds})
		status = 201
	}
	writeJSON(w, status, map[string]any{"sessionId": session.ID})
}

func (s *server) events(w http.ResponseWriter, r *http.Request, session booking.Session) {
	events, err := s.store.Events(r.Context(), session.ID)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"events": events})
}

func (s *server) createHold(w http.ResponseWriter, r *http.Request, session booking.Session) {
	var input struct {
		SectionID string `json:"sectionId"`
	}
	if !jsonBody(w, r, &input) {
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !uuidPattern.MatchString(input.SectionID) || !keyPattern.MatchString(key) {
		fail(w, r, 400, "INVALID_REQUEST", "Provide a section UUID and a 1-128 character Idempotency-Key.")
		return
	}
	result, err := s.store.CreateHold(r.Context(), booking.CreateHoldCommand{
		SessionID: session.ID, BuyerID: session.BuyerID, SectionID: input.SectionID, Key: key, RequestID: requestID(r),
	})
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	s.config.Logger.Info("hold request", "request_id", requestID(r), "hold_id", result.HoldID, "outcome", result.Code)
	switch result.Code {
	case "CREATED":
		w.Header().Set("Location", "/v1/holds/"+result.HoldID)
		writeJSON(w, 201, result)
	case "SOLD_OUT":
		fail(w, r, 409, result.Code, "This section is sold out.")
	case "IDEMPOTENCY_CONFLICT":
		fail(w, r, 409, result.Code, "This request key was already used for a different section.")
	default:
		s.storeError(w, r, fmt.Errorf("unexpected hold result %q", result.Code))
	}
}

func (s *server) getHold(w http.ResponseWriter, r *http.Request, session booking.Session) {
	holdID := r.PathValue("id")
	if !uuidPattern.MatchString(holdID) {
		fail(w, r, 404, "NOT_FOUND", "Resource not found.")
		return
	}
	hold, err := s.store.GetHold(r.Context(), session.ID, session.BuyerID, holdID)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeJSON(w, 200, hold)
}
