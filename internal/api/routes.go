// Package api monta os handlers HTTP da API pública PixelAudit.
package api

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/PixelAudit/PixelAudit/internal/orchestrator"
	"github.com/PixelAudit/PixelAudit/internal/storage"
)

// RegisterRoutes registra todos os endpoints /v1/*.
func RegisterRoutes(r *gin.Engine, v *orchestrator.Verifier, db *storage.Postgres, redis *storage.Redis) {
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok", "ts": time.Now().Unix()}) })
	r.GET("/metrics", gin.WrapH(promHandler()))

	v1 := r.Group("/v1", AuthMiddleware())
	v1.POST("/verify", handleVerify(v, redis))
	v1.GET("/verify/:id", handleGetResult(db))
}

// handleVerify aceita multipart (image + order_id + webhook_url) ou JSON base64.
func handleVerify(v *orchestrator.Verifier, redis *storage.Redis) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.GetString("tenant_id")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 800*time.Millisecond)
		defer cancel()

		var img []byte
		var orderID, webhookURL, plan string
		plan = c.GetHeader("X-Verify-Plan")

		if strings.HasPrefix(c.ContentType(), "multipart/form-data") {
			fh, err := c.FormFile("image")
			if err != nil {
				problem(c, 400, "invalid-image", "image field is required")
				return
			}
			if fh.Size > 25<<20 {
				problem(c, 413, "image-too-large", "max 25MB")
				return
			}
			f, err := fh.Open()
			if err != nil {
				problem(c, 400, "invalid-image", err.Error())
				return
			}
			defer f.Close()
			img, _ = io.ReadAll(f)
			orderID = c.PostForm("order_id")
			webhookURL = c.PostForm("webhook_url")
			if plan == "" {
				plan = c.PostForm("plan")
			}
		} else {
			var body struct {
				ImageBase64 string `json:"image_base64"`
				OrderID     string `json:"order_id"`
				WebhookURL  string `json:"webhook_url"`
				Plan        string `json:"plan"`
			}
			if err := c.BindJSON(&body); err != nil {
				problem(c, 400, "invalid-json", err.Error())
				return
			}
			img = decodeBase64(body.ImageBase64)
			orderID = body.OrderID
			webhookURL = body.WebhookURL
			if plan == "" {
				plan = body.Plan
			}
		}

		if len(img) == 0 {
			problem(c, 400, "empty-image", "image body empty")
			return
		}

		req := orchestrator.VerifyRequest{
			TenantID: tenantID,
			OrderID:  orderID,
			Plan:     plan,
			Image:    img,
		}

		// Modo async se webhook fornecido
		if webhookURL != "" {
			id, err := v.EnqueueAsync(c.Request.Context(), req)
			if err != nil {
				problem(c, 500, "enqueue-failed", err.Error())
				return
			}
			c.JSON(202, gin.H{"id": id, "status": "pending"})
			return
		}

		// Sync
		res, id, err := v.VerifySync(ctx, req)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				c.JSON(202, gin.H{"id": id, "status": "pending", "detail": "processing exceeded sync window"})
				return
			}
			problem(c, 500, "verify-failed", err.Error())
			return
		}
		res.ID = id
		c.JSON(200, res)
	}
}

func handleGetResult(db *storage.Postgres) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		res, err := db.GetResult(c.Request.Context(), id)
		if err != nil {
			problem(c, 500, "db-error", err.Error())
			return
		}
		if res == nil {
			problem(c, 404, "not-found", "verification not found")
			return
		}
		c.JSON(200, res)
	}
}

// RequestIDMiddleware injeta X-Request-ID e o expõe ao logger.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		c.Set("request_id", id)
		c.Writer.Header().Set("X-Request-ID", id)
		c.Next()
	}
}

// LoggerMiddleware loga cada requisição em JSON estruturado.
func LoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info().
			Str("request_id", c.GetString("request_id")).
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Int("status", c.Writer.Status()).
			Dur("dur", time.Since(start)).
			Msg("http")
	}
}

// RateLimitMiddleware aplica sliding window de N req/min por tenant.
func RateLimitMiddleware(r *storage.Redis, perMin int) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.GetString("tenant_id")
		if tenantID == "" {
			c.Next()
			return
		}
		allowed, remaining, reset, err := r.RateLimit(c.Request.Context(), tenantID, perMin)
		if err != nil {
			c.Next()
			return
		}
		c.Header("X-RateLimit-Limit", strconv.Itoa(perMin))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(int64(reset.Seconds()), 10))
		if !allowed {
			problem(c, 429, "rate-limited", "rate limit exceeded")
			c.Abort()
			return
		}
		c.Next()
	}
}

// AuthMiddleware valida Bearer <API_KEY> e coloca tenant_id no contexto.
// Implementação simplificada; em produção, hash + lookup no DB.
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			problem(c, 401, "missing-auth", "Authorization Bearer required")
			c.Abort()
			return
		}
		token := strings.TrimPrefix(h, "Bearer ")
		if len(token) < 8 {
			problem(c, 401, "invalid-key", "invalid API key")
			c.Abort()
			return
		}
		// TODO: DB lookup real. Placeholder derivando tenant do prefixo.
		c.Set("tenant_id", "tnt_"+token[:8])
		c.Next()
	}
}

// problem responde no formato RFC 7807.
func problem(c *gin.Context, status int, kind, detail string) {
	c.JSON(status, gin.H{
		"type":       "https://PixelAudit.io/errors/" + kind,
		"title":      strings.ReplaceAll(kind, "-", " "),
		"status":     status,
		"detail":     detail,
		"instance":   c.Request.URL.Path,
		"request_id": c.GetString("request_id"),
	})
}

func decodeBase64(s string) []byte {
	// Aceita data URLs "data:image/jpeg;base64,..."
	if i := strings.Index(s, ","); i >= 0 && strings.Contains(s[:i], "base64") {
		s = s[i+1:]
	}
	out, _ := base64Decode(s)
	return out
}

// Wrapper para permitir substituição em testes.
var base64Decode = defaultBase64Decode

func defaultBase64Decode(s string) ([]byte, error) {
	return b64.DecodeString(s)
}
