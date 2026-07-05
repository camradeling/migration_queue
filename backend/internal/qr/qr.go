// Package qr generates the registration QR code, shared by cmd/qrgen (CLI)
// and the /api/admin/qrcode endpoint so there's one implementation behind
// both entry points.
package qr

import (
	"fmt"

	"github.com/skip2/go-qrcode"
)

func RegistrationURL(baseURL string, queueID int64) string {
	return fmt.Sprintf("%s/register?queue_id=%d", baseURL, queueID)
}

func PNG(baseURL string, queueID int64, size int) ([]byte, error) {
	return qrcode.Encode(RegistrationURL(baseURL, queueID), qrcode.Medium, size)
}
