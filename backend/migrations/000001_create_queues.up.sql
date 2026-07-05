CREATE TABLE queues (
    id                     BIGSERIAL PRIMARY KEY,
    name                   TEXT NOT NULL,
    is_running             BOOLEAN NOT NULL DEFAULT FALSE,
    current_serving_number INTEGER NOT NULL DEFAULT 0
);

INSERT INTO queues (name, is_running, current_serving_number)
VALUES ('default', FALSE, 0);
