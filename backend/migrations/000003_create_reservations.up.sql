CREATE TABLE reservations (
    id                       BIGSERIAL PRIMARY KEY,
    queue_id                 BIGINT NOT NULL REFERENCES queues(id),
    full_name                TEXT NOT NULL,
    national_id              CHAR(12) NOT NULL,
    phone                    TEXT NOT NULL,
    status                   TEXT NOT NULL DEFAULT 'enqueued' CHECK (status IN ('enqueued', 'served')),
    queue_number             INTEGER NOT NULL,
    last_notified_ahead_count INTEGER,
    consent_accepted         BOOLEAN NOT NULL CHECK (consent_accepted = TRUE),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    served_at                TIMESTAMPTZ
);

CREATE UNIQUE INDEX reservations_queue_national_id_enqueued_idx
    ON reservations (queue_id, national_id)
    WHERE status = 'enqueued';

CREATE INDEX reservations_queue_status_idx ON reservations (queue_id, status);
