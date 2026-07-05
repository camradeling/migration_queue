package sms

import (
	"context"
	"log/slog"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/camradeling/migration_queue/backend/internal/db"
)

// Worker drains the durable sms_outbox table and delivers pending messages
// via Sender. The outbox row is written in the same transaction as the
// queue-state change that triggered it, so messages survive a crash between
// the state change and delivery; the worker is what actually sends them.
type Worker struct {
	DB       *sqlx.DB
	Sender   Sender
	Interval time.Duration
}

func (w *Worker) Run(ctx context.Context) {
	interval := w.Interval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.drainOnce(ctx)
		}
	}
}

func (w *Worker) drainOnce(ctx context.Context) {
	var pending []struct {
		ID    int64  `db:"id"`
		Phone string `db:"phone"`
		Msg   string `db:"message"`
	}
	err := w.DB.SelectContext(ctx, &pending, `
		SELECT o.id AS id, r.phone AS phone, o.message AS message
		FROM sms_outbox o
		JOIN reservations r ON r.id = o.reservation_id
		WHERE o.status = $1
		ORDER BY o.id
		LIMIT 100
	`, db.SMSOutboxStatusPending)
	if err != nil {
		slog.Error("sms worker: select pending failed", "err", err)
		return
	}

	for _, m := range pending {
		sendErr := w.Sender.Send(ctx, m.Phone, m.Msg)
		newStatus := db.SMSOutboxStatusSent
		if sendErr != nil {
			newStatus = db.SMSOutboxStatusFailed
			slog.Error("sms worker: send failed", "outbox_id", m.ID, "err", sendErr)
		}
		if _, err := w.DB.ExecContext(ctx, `
			UPDATE sms_outbox SET status = $1, attempts = attempts + 1 WHERE id = $2
		`, newStatus, m.ID); err != nil {
			slog.Error("sms worker: update status failed", "outbox_id", m.ID, "err", err)
		}
	}
}
