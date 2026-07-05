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

	"github.com/PixelAudit/PixelAudit/internal/email"
	"github.com/PixelAudit/PixelAudit/internal/orchestrator"
	"github.com/PixelAudit/PixelAudit/internal/storage"
)

// RegisterRoutes registra todos os endpoints /v1/*.
func RegisterRoutes(r *gin.Engine, v *orchestrator.Verifier, db *storage.Postgres, redis *storage.Redis, mailer *email.Mailer) {
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok", "ts": time.Now().Unix()}) })
	r.GET("/metrics", gin.WrapH(promHandler()))

	public := r.Group("/v1")
	public.POST("/register", handleRegister(db, mailer))
	public.POST("/login", handleLogin(db))

	protected := r.Group("/v1", AuthMiddleware())
	protected.POST("/verify", handleVerify(v, redis))
	protected.GET("/verify/:id", handleGetResult(db))
}

func handleLogin(db *storage.Postgres) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := c.BindJSON(&body); err != nil {
			problem(c, 400, "invalid-json", err.Error())
			return
		}
		body.Email = strings.TrimSpace(strings.ToLower(body.Email))
		if body.Email == "" || body.Password == "" {
			problem(c, 400, "missing-credentials", "email and password are required")
			return
		}

		user, err := db.AuthenticateUser(c.Request.Context(), body.Email, body.Password)
		if err != nil {
			problem(c, 500, "login-failed", err.Error())
			return
		}
		if user == nil {
			problem(c, 401, "invalid-credentials", "invalid email or password")
			return
		}

		c.JSON(200, gin.H{
			"user": user,
		})
	}
}

func handleRegister(db *storage.Postgres, mailer *email.Mailer) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			FullName string `json:"full_name"`
			Email    string `json:"email"`
			Company  string `json:"company"`
			Source   string `json:"source"`
		}
		if err := c.BindJSON(&body); err != nil {
			problem(c, 400, "invalid-json", err.Error())
			return
		}
		body.FullName = strings.TrimSpace(body.FullName)
		body.Email = strings.TrimSpace(strings.ToLower(body.Email))
		body.Company = strings.TrimSpace(body.Company)
		if body.FullName == "" {
			problem(c, 400, "missing-name", "full_name is required")
			return
		}
		if !strings.Contains(body.Email, "@") || !strings.Contains(body.Email, ".") {
			problem(c, 400, "invalid-email", "valid email is required")
			return
		}

		signup, err := db.UpsertPublicSignup(c.Request.Context(), body.Email, body.FullName, body.Company, body.Source)
		if err != nil {
			problem(c, 500, "signup-failed", err.Error())
			return
		}

		emailSent := false
		if mailer != nil {
			if err := mailer.SendWelcome(signup.Email, signup.FullName); err != nil {
				log.Warn().Err(err).Str("signup_id", signup.ID).Msg("welcome email failed")
			} else if mailer.Configured() {
				emailSent = true
				_ = db.MarkWelcomeEmailSent(c.Request.Context(), signup.ID)
			}
		}

		c.JSON(201, gin.H{
			"id":         signup.ID,
			"email":      signup.Email,
			"full_name":  signup.FullName,
			"company":    signup.Company,
			"email_sent": emailSent,
		})
	}
}

// handleVerify aceita multipart (image + order_id + webhook_url) ou JSON base64.
func handleVerify(v *orchestrator.Verifier, redis *storage.Redis) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID := c.GetString("tenant_id")
		ctx, cancel := context.WithTimeout(c.Request.Context(), 900*time.Millisecond)
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
				if _, enqueueErr := v.StartLocalAsync(c.Request.Context(), id, req); enqueueErr != nil {
					problem(c, 500, "local-queue-failed", enqueueErr.Error())
					return
				}
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
		if r == nil {
			c.Next()
			return
		}
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
