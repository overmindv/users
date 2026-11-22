-- +goose Up
ALTER TABLE user_outbox_events ADD COLUMN claim_token uuid;
CREATE TABLE privacy_requests (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES users(id),
 session_id uuid NOT NULL,
 security_version bigint NOT NULL,
 kind text NOT NULL CHECK(kind IN ('delete','export')),
 state text NOT NULL DEFAULT 'awaiting_confirmation' CHECK(state IN ('awaiting_confirmation','processing','completed','failed','expired')),
 receipt_hash bytea NOT NULL CHECK(octet_length(receipt_hash)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 confirm_until timestamptz NOT NULL DEFAULT clock_timestamp()+interval '15 minutes',
 expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '24 hours',
 confirmed_at timestamptz,
 profile jsonb
);
CREATE INDEX privacy_requests_user ON privacy_requests(user_id,created_at);
CREATE INDEX privacy_requests_expiry ON privacy_requests(expires_at);
CREATE UNIQUE INDEX privacy_deleted_user ON privacy_requests(user_id) WHERE kind='delete' AND confirmed_at IS NOT NULL;

CREATE TABLE privacy_owner_steps (
 request_id uuid NOT NULL REFERENCES privacy_requests(id) ON DELETE CASCADE,
 owner text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','completed','failed')),
 error_code text NOT NULL DEFAULT '',
 attempts integer NOT NULL DEFAULT 0,
 claim_token uuid,
 lease_until timestamptz,
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 cursor text NOT NULL DEFAULT '',
 watermark text NOT NULL DEFAULT '',
 pages integer NOT NULL DEFAULT 0,
 bytes bigint NOT NULL DEFAULT 0,
 PRIMARY KEY(request_id,owner)
);
CREATE INDEX privacy_owner_due ON privacy_owner_steps(available_at) WHERE state IN ('pending','running');
CREATE TABLE privacy_export_pages (
 request_id uuid NOT NULL,
 owner text NOT NULL,
 page integer NOT NULL,
 payload jsonb NOT NULL,
 PRIMARY KEY(request_id,owner,page),
 FOREIGN KEY(request_id,owner) REFERENCES privacy_owner_steps(request_id,owner) ON DELETE CASCADE
);
CREATE TABLE privacy_deletion_counter(id boolean PRIMARY KEY DEFAULT true CHECK(id),position bigint NOT NULL DEFAULT 0);
INSERT INTO privacy_deletion_counter(id) VALUES(true);
CREATE TABLE privacy_deletion_ledger (
 position bigint PRIMARY KEY,
 request_id uuid NOT NULL UNIQUE REFERENCES privacy_requests(id),
 user_id uuid NOT NULL UNIQUE,
 event jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- +goose Down
DROP TABLE privacy_deletion_ledger;
DROP TABLE privacy_deletion_counter;
DROP TABLE privacy_export_pages;
DROP TABLE privacy_owner_steps;
DROP TABLE privacy_requests;
ALTER TABLE user_outbox_events DROP COLUMN claim_token;
