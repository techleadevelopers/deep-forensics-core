// Package config concentra o carregamento e validação de variáveis de ambiente.
package config

import (
	"github.com/caarlos0/env/v11"
)

// Config agrega toda a configuração de runtime do VeriFood.
// Todas as chaves são carregadas via variáveis de ambiente.
type Config struct {
	Env             string `env:"ENV" envDefault:"development"`
	Port            string `env:"PORT" envDefault:"8080"`
	DatabaseURL     string `env:"DATABASE_URL,required"`
	RedisURL        string `env:"REDIS_URL,required"`
	NATSURL         string `env:"NATS_URL,required"`
	S3Endpoint      string `env:"S3_ENDPOINT"`
	S3Bucket        string `env:"S3_BUCKET" envDefault:"verifood-evidence"`
	S3AccessKey     string `env:"S3_ACCESS_KEY"`
	S3SecretKey     string `env:"S3_SECRET_KEY"`
	S3Region        string `env:"S3_REGION" envDefault:"us-east-1"`
	ONNXModelPath   string `env:"ONNX_MODEL_PATH" envDefault:"./models/ai_detector_v1.2.0.onnx"`
	JWTSecret       string `env:"JWT_SECRET,required"`
	RateLimitPerMin int    `env:"RATE_LIMIT_PER_MIN" envDefault:"60"`
	LogLevel        string `env:"LOG_LEVEL" envDefault:"info"`
	MaxImageBytes   int64  `env:"MAX_IMAGE_BYTES" envDefault:"26214400"`         // 25 MiB
	ResultCacheTTL  int    `env:"RESULT_CACHE_TTL_SECONDS" envDefault:"2592000"` // 30 days
	StageCacheTTL   int    `env:"STAGE_CACHE_TTL_SECONDS" envDefault:"7776000"`  // 90 days
}

// Load lê variáveis de ambiente para a struct Config.
func Load() (*Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
