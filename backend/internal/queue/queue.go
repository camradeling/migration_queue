// Package queue implements the registration/renumbering/notification logic
// described in docs/PLAN.md: assigning queue numbers, advancing the queue via
// Next, and the throttled-notification fan-out rule.
package queue

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/camradeling/migration_queue/backend/internal/db"
	"github.com/camradeling/migration_queue/backend/internal/sms"
)

var (
	ErrConsentRequired   = errors.New("consent_accepted must be true")
	ErrDuplicateEnqueued = errors.New("national_id already enqueued for this queue")
	ErrQueueNotFound     = errors.New("queue not found")
)

const uniqueViolationCode = "23505"

type Service struct {
	DB *sqlx.DB
}

func New(dbx *sqlx.DB) *Service {
	return &Service{DB: dbx}
}

// Register assigns a queue number immediately at insert time (see
// docs/PLAN.md "Renumbering"), seeding last_notified_ahead_count from the
// customer's initial ahead-count so the throttling rule in Next has a
// baseline to compare against.
func (s *Service) Register(ctx context.Context, queueID int64, fullName, nationalID, phone string, consent bool) (*db.Reservation, error) {
	if !consent {
		return nil, ErrConsentRequired
	}

	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var lockedID int64
	if err := tx.GetContext(ctx, &lockedID, `SELECT id FROM queues WHERE id = $1 FOR UPDATE`, queueID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrQueueNotFound
		}
		return nil, err
	}

	var aheadCount int
	if err := tx.GetContext(ctx, &aheadCount, `
		SELECT count(*) FROM reservations WHERE queue_id = $1 AND status = $2
	`, queueID, db.ReservationStatusEnqueued); err != nil {
		return nil, err
	}

	queueNumber := aheadCount + 1

	var res db.Reservation
	err = tx.GetContext(ctx, &res, `
		INSERT INTO reservations
			(queue_id, full_name, national_id, phone, status, queue_number, last_notified_ahead_count, consent_accepted)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING *
	`, queueID, fullName, nationalID, phone, db.ReservationStatusEnqueued, queueNumber, aheadCount, true)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == uniqueViolationCode {
			return nil, ErrDuplicateEnqueued
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &res, nil
}

type NextResult struct {
	QueueEmpty bool
	Served     *db.Reservation
}

// Next advances the queue by one: marks the lowest-numbered enqueued
// reservation as served, then re-evaluates every remaining reservation's
// ahead-count and notifies per the throttling rule (next 10 always, others
// only on >=10% movement — see docs/PLAN.md "Notification Throttling").
func (s *Service) Next(ctx context.Context, queueID int64) (*NextResult, error) {
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var lockedID int64
	if err := tx.GetContext(ctx, &lockedID, `SELECT id FROM queues WHERE id = $1 FOR UPDATE`, queueID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrQueueNotFound
		}
		return nil, err
	}

	var served db.Reservation
	err = tx.GetContext(ctx, &served, `
		SELECT * FROM reservations
		WHERE queue_id = $1 AND status = $2
		ORDER BY queue_number ASC
		LIMIT 1
		FOR UPDATE
	`, queueID, db.ReservationStatusEnqueued)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &NextResult{QueueEmpty: true}, nil
	}
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE reservations SET status = $1, served_at = now() WHERE id = $2
	`, db.ReservationStatusServed, served.ID); err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE queues SET current_serving_number = $1 WHERE id = $2
	`, served.QueueNumber, queueID); err != nil {
		return nil, err
	}

	var remaining []db.Reservation
	if err := tx.SelectContext(ctx, &remaining, `
		SELECT * FROM reservations
		WHERE queue_id = $1 AND status = $2
		ORDER BY queue_number ASC
		FOR UPDATE
	`, queueID, db.ReservationStatusEnqueued); err != nil {
		return nil, err
	}

	for i, r := range remaining {
		aheadCount := i
		if !shouldNotify(aheadCount, r.LastNotifiedAheadCount) {
			continue
		}
		if err := notify(ctx, tx, r.ID, aheadCount); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &NextResult{Served: &served}, nil
}

// shouldNotify implements the throttling rule: always notify the next 10 in
// line; beyond that, only when ahead_count has moved by >=10% since the last
// notification.
func shouldNotify(aheadCount int, lastNotified sql.NullInt64) bool {
	if aheadCount < 10 {
		return true
	}
	if !lastNotified.Valid {
		return true
	}
	last := int(lastNotified.Int64)
	threshold := int(math.Ceil(0.10 * float64(last)))
	if threshold < 1 {
		threshold = 1
	}
	diff := aheadCount - last
	if diff < 0 {
		diff = -diff
	}
	return diff >= threshold
}

func notify(ctx context.Context, tx *sqlx.Tx, reservationID int64, aheadCount int) error {
	message := sms.PositionMessage(aheadCount)
	if aheadCount == 0 {
		message = sms.YourTurnMessage()
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sms_outbox (reservation_id, message) VALUES ($1, $2)
	`, reservationID, message); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE reservations SET last_notified_ahead_count = $1 WHERE id = $2
	`, aheadCount, reservationID)
	return err
}

// Start renumbers all enqueued reservations 1..N in FIFO (created_at) order
// and does a full SMS fan-out, bypassing the throttling rule (see
// docs/PLAN.md "Renumbering" and "Notification Throttling").
func (s *Service) Start(ctx context.Context, queueID int64) error {
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var lockedID int64
	if err := tx.GetContext(ctx, &lockedID, `SELECT id FROM queues WHERE id = $1 FOR UPDATE`, queueID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrQueueNotFound
		}
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE queues SET is_running = true WHERE id = $1`, queueID); err != nil {
		return err
	}

	var enqueued []db.Reservation
	if err := tx.SelectContext(ctx, &enqueued, `
		SELECT * FROM reservations
		WHERE queue_id = $1 AND status = $2
		ORDER BY created_at ASC
		FOR UPDATE
	`, queueID, db.ReservationStatusEnqueued); err != nil {
		return err
	}

	for i, r := range enqueued {
		queueNumber := i + 1
		aheadCount := i
		if _, err := tx.ExecContext(ctx, `
			UPDATE reservations
			SET queue_number = $1, last_notified_ahead_count = $2
			WHERE id = $3
		`, queueNumber, aheadCount, r.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sms_outbox (reservation_id, message) VALUES ($1, $2)
		`, r.ID, sms.QueueStartedMessage(queueNumber)); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Stop marks the queue closed and sends a full-fan-out "closed for today"
// notice to everyone still enqueued; their positions are preserved.
func (s *Service) Stop(ctx context.Context, queueID int64) error {
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var lockedID int64
	if err := tx.GetContext(ctx, &lockedID, `SELECT id FROM queues WHERE id = $1 FOR UPDATE`, queueID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrQueueNotFound
		}
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE queues SET is_running = false WHERE id = $1`, queueID); err != nil {
		return err
	}

	var enqueuedIDs []int64
	if err := tx.SelectContext(ctx, &enqueuedIDs, `
		SELECT id FROM reservations WHERE queue_id = $1 AND status = $2
	`, queueID, db.ReservationStatusEnqueued); err != nil {
		return err
	}

	for _, id := range enqueuedIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sms_outbox (reservation_id, message) VALUES ($1, $2)
		`, id, sms.QueueClosedMessage()); err != nil {
			return err
		}
	}

	return tx.Commit()
}
