package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/verifood/verifood/internal/analyzer"
	"github.com/verifood/verifood/internal/config"
	"github.com/verifood/verifood/internal/model"
	"github.com/verifood/verifood/internal/orchestrator"
	"github.com/verifood/verifood/internal/queue"
	"github.com/verifood/verifood/internal/storage"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("config")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := storage.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("postgres")
	}
	defer db.Close()

	s3, err := storage.NewS3(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("s3")
	}

	redis, err := storage.NewRedis(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatal().Err(err).Msg("redis")
	}
	defer redis.Close()

	nc, err := queue.NewNATS(cfg.NATSURL)
	if err != nil {
		log.Fatal().Err(err).Msg("nats")
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatal().Err(err).Msg("jetstream")
	}

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     "VERIFY",
		Subjects: []string{"verify.>"},
		Storage:  jetstream.FileStorage,
		MaxAge:   24 * time.Hour,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("stream")
	}

	meta := analyzer.NewMetadataAnalyzer()
	ela := analyzer.NewELAAnalyzer(0.06)
	freq := analyzer.NewFrequencyAnalyzer()
	ai, err := analyzer.NewAIDetector(cfg.ONNXModelPath)
	if err != nil {
		log.Warn().Err(err).Msg("AI detector disabled")
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

	handler := func(msg jetstream.Msg) {
		start := time.Now()
		var evt model.VerificationRequestedEvent
		if err := json.Unmarshal(msg.Data(), &evt); err != nil {
			log.Error().Err(err).Msg("invalid event")
			_ = msg.Term()
			return
		}

		logger := log.With().Str("verification_id", evt.VerificationID).Logger()
		logger.Info().
			Str("subject", msg.Subject()).
			Str("plan", evt.Plan).
			Str("profile", evt.Profile).
			Msg("processing")

		result, err := verifier.ProcessAsyncEvent(ctx, evt)
		if err != nil {
			logger.Error().Err(err).Msg("process")
			_ = msg.NakWithDelay(5 * time.Second)
			return
		}
		result.ProcessingTimeMs = int(time.Since(start).Milliseconds())
		if err := db.SaveResult(ctx, evt.VerificationID, result); err != nil {
			logger.Error().Err(err).Msg("save result")
			_ = msg.Nak()
			return
		}

		payload, _ := json.Marshal(model.WebhookPayload{
			VerificationID: evt.VerificationID,
			TenantID:       evt.TenantID,
			Result:         result,
		})
		_ = nc.Publish("webhook.dispatch", payload)

		_ = msg.Ack()
		logger.Info().
			Int("ms", result.ProcessingTimeMs).
			Float64("confidence", result.Confidence).
			Str("recommendation", result.Recommendation).
			Msg("completed")
	}

	consumeContexts := startConsumers(ctx, stream, handler)
	defer func() {
		for _, consumeCtx := range consumeContexts {
			consumeCtx.Stop()
		}
	}()

	log.Info().Msg("VeriFood worker running")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("shutting down worker")
}

func startConsumers(ctx context.Context, stream jetstream.Stream, handler jetstream.MessageHandler) []jetstream.ConsumeContext {
	subjects := []struct {
		durable string
		subject string
		pending int
	}{
		{"worker_paid_high", orchestrator.SubjectPaidHigh, 100},
		{"worker_paid_normal", orchestrator.SubjectPaidNorm, 75},
		{"worker_free_low", orchestrator.SubjectFreeLow, 25},
		{"worker_retry", orchestrator.SubjectRetry, 25},
	}

	var contexts []jetstream.ConsumeContext
	for _, s := range subjects {
		cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
			Durable:       s.durable,
			AckPolicy:     jetstream.AckExplicitPolicy,
			MaxAckPending: s.pending,
			AckWait:       30 * time.Second,
			MaxDeliver:    3,
			FilterSubject: s.subject,
		})
		if err != nil {
			log.Fatal().Err(err).Str("subject", s.subject).Msg("consumer")
		}
		consumeCtx, err := cons.Consume(handler, jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
			log.Error().Err(err).Str("subject", s.subject).Msg("consume error")
		}))
		if err != nil {
			log.Fatal().Err(err).Str("subject", s.subject).Msg("consume")
		}
		contexts = append(contexts, consumeCtx)
	}
	return contexts
}
