package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/jmoiron/sqlx"
	"github.com/nyaruka/phonenumbers"
	"golang.org/x/crypto/bcrypt"

	"github.com/camradeling/migration_queue/backend/internal/config"
	dbmodels "github.com/camradeling/migration_queue/backend/internal/db"
	"github.com/camradeling/migration_queue/backend/internal/qr"
	"github.com/camradeling/migration_queue/backend/internal/queue"
)

type Handlers struct {
	cfg      *config.Config
	db       *sqlx.DB
	queue    *queue.Service
	validate *validator.Validate
}

func newHandlers(cfg *config.Config, dbx *sqlx.DB, qsvc *queue.Service) *Handlers {
	return &Handlers{cfg: cfg, db: dbx, queue: qsvc, validate: validator.New()}
}

// resolveQueueID reads an optional ?queue_id= query param, falling back to
// the single seeded queue for v1 (schema already supports many; only one
// row exists today).
func (h *Handlers) resolveQueueID(c *gin.Context) (int64, error) {
	if raw := c.Query("queue_id"); raw != "" {
		return strconv.ParseInt(raw, 10, 64)
	}
	var id int64
	err := h.db.Get(&id, `SELECT id FROM queues ORDER BY id LIMIT 1`)
	return id, err
}

type registerRequest struct {
	FullName        string `json:"full_name" validate:"required,min=2,max=255"`
	NationalID      string `json:"national_id" validate:"required,len=12,numeric"`
	Phone           string `json:"phone" validate:"required"`
	ConsentAccepted bool   `json:"consent_accepted" validate:"required"`
}

func (h *Handlers) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.validate.Struct(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	num, err := phonenumbers.Parse(req.Phone, "KZ")
	if err != nil || !phonenumbers.IsValidNumber(num) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid phone number"})
		return
	}
	e164 := phonenumbers.Format(num, phonenumbers.E164)

	queueID, err := h.resolveQueueID(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no queue configured"})
		return
	}

	res, err := h.queue.Register(c.Request.Context(), queueID, req.FullName, req.NationalID, e164, req.ConsentAccepted)
	switch {
	case errors.Is(err, queue.ErrDuplicateEnqueued):
		c.JSON(http.StatusConflict, gin.H{"error": "already enqueued"})
		return
	case errors.Is(err, queue.ErrConsentRequired):
		c.JSON(http.StatusBadRequest, gin.H{"error": "consent required"})
		return
	case errors.Is(err, queue.ErrQueueNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "queue not found"})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "registration failed"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"reservation_id": res.ID,
		"queue_number":   res.QueueNumber,
	})
}

type loginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

func (h *Handlers) AdminLogin(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.validate.Struct(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var admin dbmodels.Admin
	err := h.db.Get(&admin, `SELECT * FROM admins WHERE username = $1`, req.Username)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := issueToken(h.cfg.JWTSecret, admin.ID, admin.Username)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not issue token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"token": token})
}

func (h *Handlers) AdminNext(c *gin.Context) {
	queueID, err := h.resolveQueueID(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no queue configured"})
		return
	}
	result, err := h.queue.Next(c.Request.Context(), queueID)
	if errors.Is(err, queue.ErrQueueNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "queue not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "next failed"})
		return
	}
	if result.QueueEmpty {
		c.JSON(http.StatusOK, gin.H{"status": "queue_empty"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":                "ok",
		"served_reservation_id": result.Served.ID,
		"served_queue_number":   result.Served.QueueNumber,
	})
}

func (h *Handlers) AdminStart(c *gin.Context) {
	queueID, err := h.resolveQueueID(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no queue configured"})
		return
	}
	if err := h.queue.Start(c.Request.Context(), queueID); err != nil {
		if errors.Is(err, queue.ErrQueueNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "queue not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "start failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handlers) AdminStop(c *gin.Context) {
	queueID, err := h.resolveQueueID(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no queue configured"})
		return
	}
	if err := h.queue.Stop(c.Request.Context(), queueID); err != nil {
		if errors.Is(err, queue.ErrQueueNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "queue not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stop failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

type queueStats struct {
	CurrentServingNumber int     `json:"current_serving_number"`
	IsRunning            bool    `json:"is_running"`
	TotalEnqueued        int     `json:"total_enqueued"`
	AvgServingSeconds    float64 `json:"avg_serving_seconds"`
}

func (h *Handlers) fetchStats(queueID int64) (*queueStats, error) {
	var q dbmodels.Queue
	if err := h.db.Get(&q, `SELECT * FROM queues WHERE id = $1`, queueID); err != nil {
		return nil, err
	}

	var totalEnqueued int
	if err := h.db.Get(&totalEnqueued, `
		SELECT count(*) FROM reservations WHERE queue_id = $1 AND status = $2
	`, queueID, dbmodels.ReservationStatusEnqueued); err != nil {
		return nil, err
	}

	var avgSeconds float64
	if err := h.db.Get(&avgSeconds, `
		SELECT COALESCE(avg(extract(epoch FROM diff)), 0) FROM (
			SELECT served_at - lag(served_at) OVER (ORDER BY served_at) AS diff
			FROM reservations
			WHERE queue_id = $1 AND status = $2 AND served_at IS NOT NULL
		) t WHERE diff IS NOT NULL
	`, queueID, dbmodels.ReservationStatusServed); err != nil {
		return nil, err
	}

	return &queueStats{
		CurrentServingNumber: q.CurrentServingNumber,
		IsRunning:            q.IsRunning,
		TotalEnqueued:        totalEnqueued,
		AvgServingSeconds:    avgSeconds,
	}, nil
}

func (h *Handlers) AdminStats(c *gin.Context) {
	queueID, err := h.resolveQueueID(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no queue configured"})
		return
	}
	stats, err := h.fetchStats(queueID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stats failed"})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func (h *Handlers) AdminQRCode(c *gin.Context) {
	queueID, err := h.resolveQueueID(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no queue configured"})
		return
	}
	png, err := qr.PNG(h.cfg.RegistrationBaseURL, queueID, 256)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "qrcode generation failed"})
		return
	}
	c.Data(http.StatusOK, "image/png", png)
}
