-- Credentials belong to buyers. Multiple buyers can share one match and still
-- have independent idempotency namespaces and booking visibility.
ALTER TABLE buyers ADD COLUMN token_hash text UNIQUE;

UPDATE buyers b SET token_hash = s.token_hash
FROM demo_sessions s
WHERE b.session_id = s.id AND b.is_primary;

ALTER TABLE demo_sessions DROP COLUMN token_hash;
CREATE INDEX buyers_by_session ON buyers (session_id);
