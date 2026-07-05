// Package config concentra o carregamento e validação de variáveis de ambiente.
package config

import (
	"bufio"
	"os"
	"strings"

	"github.com/caarlos0/env/v11"
)

// Config agrega toda a configuração de runtime do PixelAudit.
// Todas as chaves são carregadas via variáveis de ambiente.
type Config struct {
	Env             string `env:"ENV" envDefault:"development"`
	Port            string `env:"PORT" envDefault:"8080"`
	DatabaseURL     string `env:"DATABASE_URL,required"`
	RedisURL        string `env:"REDIS_URL"`
	NATSURL         string `env:"NATS_URL"`
	S3Endpoint      string `env:"S3_ENDPOINT"`
	S3Bucket        string `env:"S3_BUCKET" envDefault:"pixelaudit-evidence"`
	S3AccessKey     string `env:"S3_ACCESS_KEY"`
	S3SecretKey     string `env:"S3_SECRET_KEY"`
	S3Region        string `env:"S3_REGION" envDefault:"us-east-1"`
	S3LocalDir      string `env:"S3_LOCAL_DIR" envDefault:"./tmp/evidence"`
	ONNXModelPath   string `env:"ONNX_MODEL_PATH" envDefault:"./models/ai_detector_v1.2.0.onnx"`
	JWTSecret       string `env:"JWT_SECRET" envDefault:"dev-only-change-me"`
	RateLimitPerMin int    `env:"RATE_LIMIT_PER_MIN" envDefault:"60"`
	LogLevel        string `env:"LOG_LEVEL" envDefault:"info"`
	MaxImageBytes   int64  `env:"MAX_IMAGE_BYTES" envDefault:"26214400"`         // 25 MiB
	ResultCacheTTL  int    `env:"RESULT_CACHE_TTL_SECONDS" envDefault:"2592000"` // 30 days
	StageCacheTTL   int    `env:"STAGE_CACHE_TTL_SECONDS" envDefault:"7776000"`  // 90 days
	SMTPHost        string `env:"SMTP_HOST"`
	SMTPPort        int    `env:"SMTP_PORT" envDefault:"587"`
	SMTPUser        string `env:"SMTP_USER"`
	SMTPPass        string `env:"SMTP_PASS"`
	SMTPSecure      bool   `env:"SMTP_SECURE" envDefault:"true"`
	SMTPFromEmail   string `env:"SMTP_FROM_EMAIL"`
	SMTPFromName    string `env:"SMTP_FROM_NAME" envDefault:"PixelAudit"`
}

// Load lê variáveis de ambiente para a struct Config.
func Load() (*Config, error) {
	_ = loadDotEnv(".env")
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		_ = os.Setenv(key, value)
	}
	return scanner.Err()
}
