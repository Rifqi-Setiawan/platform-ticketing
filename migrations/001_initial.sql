CREATE TABLE demo_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash text NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT (statement_timestamp() + interval '24 hours')
);
CREATE INDEX sessions_due ON demo_sessions (expires_at);

CREATE TABLE buyers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES demo_sessions(id) ON DELETE CASCADE,
    is_primary boolean NOT NULL DEFAULT false
);
CREATE UNIQUE INDEX buyers_session_primary ON buyers (session_id) WHERE is_primary;

CREATE TABLE events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES demo_sessions(id) ON DELETE CASCADE,
    name text NOT NULL,
    is_fixture boolean NOT NULL DEFAULT false,
    starts_at timestamptz NOT NULL DEFAULT (date_trunc('day', now()) + interval '7 days 12 hours')
);
CREATE INDEX events_session ON events (session_id);

CREATE TABLE sections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id uuid NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    name text NOT NULL,
    capacity integer NOT NULL CHECK (capacity > 0),
    available integer NOT NULL CHECK (available BETWEEN 0 AND capacity),
    price_idr bigint NOT NULL CHECK (price_idr >= 0),
    UNIQUE (event_id, name)
);

CREATE TABLE holds (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    buyer_id uuid NOT NULL REFERENCES buyers(id) ON DELETE CASCADE,
    section_id uuid NOT NULL REFERENCES sections(id) ON DELETE CASCADE,
    price_idr bigint NOT NULL CHECK (price_idr >= 0),
    status text NOT NULL CHECK (status IN ('HELD', 'CONFIRMED', 'EXPIRED')),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    finalized_at timestamptz,
    CHECK ((status = 'HELD') = (finalized_at IS NULL)),
    CHECK (expires_at >= created_at)
);
CREATE INDEX holds_due ON holds (expires_at, id) WHERE status = 'HELD';
CREATE INDEX holds_by_section ON holds (section_id, status);
CREATE INDEX holds_by_buyer ON holds (buyer_id);

CREATE TABLE tickets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    hold_id uuid NOT NULL UNIQUE REFERENCES holds(id) ON DELETE CASCADE,
    issued_at timestamptz NOT NULL
);

CREATE TABLE idempotency_keys (
    buyer_id uuid NOT NULL REFERENCES buyers(id) ON DELETE CASCADE,
    operation text NOT NULL,
    key text NOT NULL,
    section_id uuid NOT NULL,
    http_status integer,
    result jsonb,
    PRIMARY KEY (buyer_id, operation, key)
);

CREATE TABLE payment_events (
    provider text NOT NULL,
    event_id text NOT NULL,
    hold_id uuid NOT NULL REFERENCES holds(id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
    amount_idr bigint NOT NULL,
    result jsonb,
    PRIMARY KEY (provider, event_id)
);
CREATE INDEX payment_events_by_hold ON payment_events (hold_id);

CREATE TABLE hold_transitions (
    hold_id uuid NOT NULL REFERENCES holds(id) ON DELETE CASCADE,
    sequence integer NOT NULL CHECK (sequence IN (1, 2)),
    from_status text,
    to_status text NOT NULL,
    cause text NOT NULL,
    occurred_at timestamptz NOT NULL,
    request_id text NOT NULL,
    PRIMARY KEY (hold_id, sequence)
);
