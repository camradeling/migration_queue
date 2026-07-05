package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	// Registration page and admin app are served from the same backend
	// origin in v1; revisit if the admin app ever runs from a different
	// origin.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WSStats streams queueStats every couple seconds. The JWT can't ride an
// Authorization header on a WebSocket handshake, so it's passed explicitly
// as a query param (see docs/PLAN.md).
func (h *Handlers) WSStats(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token query param"})
		return
	}
	if _, err := parseToken(h.cfg.JWTSecret, token); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	queueID, err := h.resolveQueueID(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no queue configured"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		stats, err := h.fetchStats(queueID)
		if err != nil {
			return
		}
		if err := conn.WriteJSON(stats); err != nil {
			return
		}
		select {
		case <-ticker.C:
		case <-c.Request.Context().Done():
			return
		}
	}
}
