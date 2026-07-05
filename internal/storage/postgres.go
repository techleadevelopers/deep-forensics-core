// Package storage encapsula clientes Postgres, Redis e S3.
package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/verifood/verifood/internal/model"
)

// Postgres é o wrapper em cima do pgxpool.
type Postgres struct{ Pool *pgxpool.Pool }

// NewPostgres inicializa o pool e valida a conexão.
func NewPostgres(ctx context.Context, url string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 20
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return &Postgres{Pool: pool}, nil
}

// Close libera o pool.
func (p *Postgres) Close() { p.Pool.Close() }

// CreatePending grava uma linha em status=pending antes do processamento.
func (p *Postgres) CreatePending(ctx context.Context, id, tenantID, orderID, sha256 string) error {
	_, err := p.Pool.Exec(ctx, `
		INSERT INTO verifications (id, tenant_id, order_id, sha256, status, created_at)
		VALUES ($1, $2, NULLIF($3, ''), decode($4, 'hex'), 'pending', now())
		ON CONFLICT (id) DO NOTHING`,
		id, tenantID, orderID, sha256)
	return err
}

// SaveResult persiste o resultado final e marca o status como completed.
func (p *Postgres) SaveResult(ctx context.Context, id string, res *model.VerificationResult) error {
	analysis, _ := json.Marshal(res.Analysis)
	versions, _ := json.Marshal(res.ModelVersions)
	scores, _ := json.Marshal(res.Scores)
	_, err := p.Pool.Exec(ctx, `
		INSERT INTO verifications (id, status, authentic, confidence, recommendation, priority,
		                          analysis, model_versions, processing_time_ms, completed_at, created_at)
		VALUES ($1, 'completed', $2, $3, $4, $5, $6, $7, $8, now(), now())
		ON CONFLICT (id) DO UPDATE SET
		  status = 'completed',
		  authentic = EXCLUDED.authentic,
		  confidence = EXCLUDED.confidence,
		  recommendation = EXCLUDED.recommendation,
		  priority = EXCLUDED.priority,
		  analysis = EXCLUDED.analysis,
		  model_versions = EXCLUDED.model_versions,
		  processing_time_ms = EXCLUDED.processing_time_ms,
		  completed_at = now()`,
		id, res.Authentic, res.Confidence, res.Recommendation, res.Priority,
		analysis, versions, res.ProcessingTimeMs)
	_ = scores
	return err
}

// GetResult devolve o resultado por id.
func (p *Postgres) GetResult(ctx context.Context, id string) (*model.VerificationResult, error) {
	row := p.Pool.QueryRow(ctx, `
		SELECT id, status, authentic, confidence, recommendation, priority,
		       analysis, model_versions, processing_time_ms, completed_at
		FROM verifications WHERE id = $1`, id)
	var (
		res       model.VerificationResult
		status    string
		analysis  []byte
		versions  []byte
		completed *time.Time
	)
	if err := row.Scan(&res.ID, &status, &res.Authentic, &res.Confidence,
		&res.Recommendation, &res.Priority, &analysis, &versions,
		&res.ProcessingTimeMs, &completed); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	res.ID = id
	if completed != nil {
		res.Timestamp = *completed
	}
	_ = json.Unmarshal(analysis, &res.Analysis)
	_ = json.Unmarshal(versions, &res.ModelVersions)
	return &res, nil
}
