package db

import (
	"database/sql"
	"time"
)

type Queue struct {
	ID                   int64  `db:"id"`
	Name                 string `db:"name"`
	IsRunning            bool   `db:"is_running"`
	CurrentServingNumber int    `db:"current_serving_number"`
}

type Reservation struct {
	ID                     int64         `db:"id"`
	QueueID                int64         `db:"queue_id"`
	FullName               string        `db:"full_name"`
	NationalID             string        `db:"national_id"`
	Phone                  string        `db:"phone"`
	Status                 string        `db:"status"`
	QueueNumber            int           `db:"queue_number"`
	LastNotifiedAheadCount sql.NullInt64 `db:"last_notified_ahead_count"`
	ConsentAccepted        bool          `db:"consent_accepted"`
	CreatedAt              time.Time     `db:"created_at"`
	ServedAt               sql.NullTime  `db:"served_at"`
}

type Admin struct {
	ID           int64  `db:"id"`
	Username     string `db:"username"`
	PasswordHash string `db:"password_hash"`
}

const (
	ReservationStatusEnqueued = "enqueued"
	ReservationStatusServed   = "served"
)

const (
	SMSOutboxStatusPending = "pending"
	SMSOutboxStatusSent    = "sent"
	SMSOutboxStatusFailed  = "failed"
)
