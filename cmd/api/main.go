package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/PixelAudit/PixelAudit/internal/analyzer"
	"github.com/PixelAudit/PixelAudit/internal/api"
	"github.com/PixelAudit/PixelAudit/internal/config"
	"github.com/PixelAudit/PixelAudit/internal/orchestrator"
	"github.com/PixelAudit/PixelAudit/internal/queue"
	"github.com/PixelAudit/PixelAudit/internal/storage"
)

// main é o entrypoint da API HTTP do PixelAudit.
// Bootstrap: config → logger → storage → queue → analisadores → orchestrator → HTTP server.
func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Dependencies
	db, err := storage.NewPostgres(rootCtx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("postgres")
	}
	defer db.Close()

	redis, err := storage.NewRedis(rootCtx, cfg.RedisURL)
	if err != nil {
		log.Fatal().Err(err).Msg("redis")
	}
	defer redis.Close()

	s3, err := storage.NewS3(rootCtx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("s3")
	}

	nc, err := queue.NewNATS(cfg.NATSURL)
	if err != nil {
		log.Fatal().Err(err).Msg("nats")
	}
	defer nc.Close()

	// Analyzers
	meta := analyzer.NewMetadataAnalyzer()
	ela := analyzer.NewELAAnalyzer(0.06)
	freq := analyzer.NewFrequencyAnalyzer()
	ai, err := analyzer.NewAIDetector(cfg.ONNXModelPath)
	if err != nil {
		log.Warn().Err(err).Msg("AI detector disabled (model not loaded)")
	}

	verifier := orchestrator.New(
		meta,
		ela,
		ai,
		freq,
		db,
		s3,
		redis,
		nc,
		time.Duration(cfg.ResultCacheTTL)*time.Second,
		time.Duration(cfg.StageCacheTTL)*time.Second,
	)

	// HTTP
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery(), api.RequestIDMiddleware(), api.LoggerMiddleware(), api.RateLimitMiddleware(redis, cfg.RateLimitPerMin))
	r.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "OPTIONS"},
		AllowHeaders: []string{"Authorization", "Content-Type", "Idempotency-Key"},
	}))
	api.RegisterRoutes(r, verifier, db, redis)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info().Str("addr", srv.Addr).Msg("PixelAudit API listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("http server")
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Info().Msg("bye")
}
