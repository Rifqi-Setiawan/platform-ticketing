ALTER TABLE holds
    ADD COLUMN quantity smallint NOT NULL DEFAULT 1 CHECK (quantity BETWEEN 1 AND 4),
    ADD COLUMN display_name text NOT NULL DEFAULT '' CHECK (char_length(display_name) <= 60);

ALTER TABLE idempotency_keys
    ADD COLUMN quantity smallint NOT NULL DEFAULT 1 CHECK (quantity BETWEEN 1 AND 4),
    ADD COLUMN display_name text NOT NULL DEFAULT '';

ALTER TABLE tickets
    ADD COLUMN ordinal smallint NOT NULL DEFAULT 1 CHECK (ordinal BETWEEN 1 AND 4),
    DROP CONSTRAINT tickets_hold_id_key,
    ADD UNIQUE (hold_id, ordinal);
