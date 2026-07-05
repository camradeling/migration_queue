package sms

import (
	"context"
	"log/slog"
)

// Sender delivers a single SMS message to a phone number. Implementations
// are swappable: a concrete KZ gateway adapter for production, ConsoleSender
// for local dev.
type Sender interface {
	Send(ctx context.Context, phone, message string) error
}

// ConsoleSender logs messages instead of sending them; used in dev so the
// notification flow (throttling, fan-out) can be observed without a real
// gateway.
type ConsoleSender struct{}

func (ConsoleSender) Send(_ context.Context, phone, message string) error {
	slog.Info("sms (console adapter)", "phone", phone, "message", message)
	return nil
}
