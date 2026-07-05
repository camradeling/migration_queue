package api

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"

	"github.com/camradeling/migration_queue/backend/internal/config"
	"github.com/camradeling/migration_queue/backend/internal/queue"
)

func NewRouter(cfg *config.Config, dbx *sqlx.DB, qsvc *queue.Service) *gin.Engine {
	h := newHandlers(cfg, dbx, qsvc)
	limiter := newLoginRateLimiter()

	r := gin.Default()
	r.Use(cors.Default())

	r.POST("/api/register", h.Register)
	r.POST("/api/admin/login", limiter.middleware(), h.AdminLogin)
	r.GET("/ws/stats", h.WSStats)

	admin := r.Group("/api/admin")
	admin.Use(authMiddleware(cfg.JWTSecret))
	{
		admin.POST("/next", h.AdminNext)
		admin.POST("/start", h.AdminStart)
		admin.POST("/stop", h.AdminStop)
		admin.GET("/stats", h.AdminStats)
		admin.GET("/qrcode", h.AdminQRCode)
	}

	return r
}
