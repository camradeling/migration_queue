// Command qrgen prints a registration QR code PNG to a file, reusing the
// same encoding logic as the /api/admin/qrcode endpoint (internal/qr) so
// operators can (re)generate/print codes without going through the API.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/camradeling/migration_queue/backend/internal/qr"
)

func main() {
	baseURL := flag.String("base-url", "http://localhost:8080", "registration page base URL")
	queueID := flag.Int64("queue-id", 1, "queue ID to encode")
	size := flag.Int("size", 256, "PNG size in pixels")
	out := flag.String("out", "queue.png", "output PNG file path")
	flag.Parse()

	png, err := qr.PNG(*baseURL, *queueID, *size)
	if err != nil {
		fmt.Fprintln(os.Stderr, "qrgen:", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*out, png, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "qrgen:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%s)\n", *out, qr.RegistrationURL(*baseURL, *queueID))
}
