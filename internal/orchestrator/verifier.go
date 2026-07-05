package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/oklog/ulid/v2"

	"github.com/PixelAudit/PixelAudit/internal/analyzer"
	"github.com/PixelAudit/PixelAudit/internal/model"
	"github.com/PixelAudit/PixelAudit/internal/storage"
)

// Verifier orchestrates upload, cache/dedup, analysis, persistence, and queueing.
type Verifier struct {
	meta      *analyzer.MetadataAnalyzer
	ela       *analyzer.ELAAnalyzer
	ai        *analyzer.AIDetector
	freq      *analyzer.FrequencyAnalyzer
	fusion    *analyzer.Fusion
	db        *storage.Postgres
	s3        *storage.S3
	redis     *storage.Redis
	nc        *nats.Conn
	resultTTL time.Duration
	stageTTL  time.Duration
}

func New(
	meta *analyzer.MetadataAnalyzer,
	ela *analyzer.ELAAnalyzer,
	ai *analyzer.AIDetector,
	freq *analyzer.FrequencyAnalyzer,
	db *storage.Postgres,
	s3 *storage.S3,
	redis *storage.Redis,
	nc *nats.Conn,
	resultTTL time.Duration,
	stageTTL time.Duration,
) *Verifier {
	return &Verifier{
		meta:      meta,
		ela:       ela,
		ai:        ai,
		freq:      freq,
		fusion:    analyzer.NewFusion(analyzer.DefaultWeights),
		db:        db,
		s3:        s3,
		redis:     redis,
		nc:        nc,
		resultTTL: resultTTL,
		stageTTL:  stageTTL,
	}
}

type VerifyRequest struct {
	TenantID string
	OrderID  string
	Plan     string
	Image    []byte
}

func (v *Verifier) VerifySync(ctx context.Context, req VerifyRequest) (*model.VerificationResult, string, error) {
	id := "ver_" + ulid.Make().String()
	sha := imageSHA256(req.Image)
	plan := NormalizePlan(req.Plan)
	profile := PipelineProfile(plan)

	if cached := v.cachedResult(ctx, req.TenantID, sha, profile); cached != nil {
		return cached, cached.ID, nil
	}

	key := originalKey(req.TenantID, sha)
	if _, err := v.s3.Put(ctx, key, req.Image, "image/jpeg"); err != nil {
		return nil, id, err
	}

	res, err := v.runPipeline(ctx, id, req.TenantID, sha, plan, profile, req.Image)
	if err != nil {
		return nil, id, err
	}
	res.ID = id
	res.Timestamp = time.Now().UTC()

	_ = v.db.SaveResult(ctx, id, res)
	v.cacheResult(ctx, req.TenantID, sha, profile, res)
	return res, id, nil
}

func (v *Verifier) EnqueueAsync(ctx context.Context, req VerifyRequest) (string, error) {
	id := "ver_" + ulid.Make().String()
	sha := imageSHA256(req.Image)
	plan := NormalizePlan(req.Plan)
	profile := PipelineProfile(plan)

	if cached := v.cachedResult(ctx, req.TenantID, sha, profile); cached != nil {
		clone := *cached
		clone.ID = id
		clone.Timestamp = time.Now().UTC()
		_ = v.db.SaveResult(ctx, id, &clone)
		return id, nil
	}

	key := originalKey(req.TenantID, sha)
	if _, err := v.s3.Put(ctx, key, req.Image, "image/jpeg"); err != nil {
		return "", err
	}

	evt := model.VerificationRequestedEvent{
		VerificationID: id,
		TenantID:       req.TenantID,
		S3Key:          key,
		SHA256:         sha,
		OrderID:        req.OrderID,
		Plan:           plan,
		Profile:        profile,
		RequestedAt:    time.Now().UTC(),
	}
	payload, _ := json.Marshal(evt)
	if err := v.nc.Publish(QueueSubject(plan), payload); err != nil {
		return "", err
	}
	_ = v.db.CreatePending(ctx, id, req.TenantID, req.OrderID, sha)
	return id, nil
}

func (v *Verifier) ProcessAsyncEvent(ctx context.Context, evt model.VerificationRequestedEvent) (*model.VerificationResult, error) {
	plan := NormalizePlan(evt.Plan)
	profile := evt.Profile
	if profile == "" {
		profile = PipelineProfile(plan)
	}

	if cached := v.cachedResult(ctx, evt.TenantID, evt.SHA256, profile); cached != nil {
		clone := *cached
		clone.ID = evt.VerificationID
		clone.Timestamp = time.Now().UTC()
		if err := v.db.SaveResult(ctx, evt.VerificationID, &clone); err != nil {
			return nil, err
		}
		return &clone, nil
	}

	img, err := v.s3.Get(ctx, evt.S3Key)
	if err != nil {
		return nil, err
	}
	res, err := v.runPipeline(ctx, evt.VerificationID, evt.TenantID, evt.SHA256, plan, profile, img)
	if err != nil {
		return nil, err
	}
	res.ID = evt.VerificationID
	res.Timestamp = time.Now().UTC()
	if err := v.db.SaveResult(ctx, evt.VerificationID, res); err != nil {
		return nil, err
	}
	v.cacheResult(ctx, evt.TenantID, evt.SHA256, profile, res)
	return res, nil
}

func (v *Verifier) runPipeline(ctx context.Context, verificationID, tenantID, sha, plan, profile string, img []byte) (*model.VerificationResult, error) {
	precheck := imagePrecheck(img)

	var metaRes *model.MetadataResult
	var elaRes *model.ELAResult
	done := make(chan error, 2)
	go func() {
		var err error
		metaRes, err = v.metadata(ctx, sha, img)
		done <- err
	}()
	go func() {
		var err error
		elaRes, err = v.elaResult(ctx, sha, img)
		done <- err
	}()
	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				return nil, err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if elaRes != nil && len(elaRes.HeatmapPNG) > 0 {
		key := heatmapKey(plan, tenantID, verificationID)
		if url, err := v.s3.Put(ctx, key, elaRes.HeatmapPNG, "image/png"); err == nil {
			elaRes.HeatmapURL = url
		}
	}

	var aiRes *model.AIResult
	var freqRes *model.FrequencyResult
	if profile == ProfileFull {
		var err error
		freqRes, err = v.frequency(ctx, sha, img)
		if err != nil {
			return nil, err
		}
		if v.ai != nil {
			aiRes, err = v.aiResult(ctx, sha, img)
			if err != nil {
				return nil, err
			}
		}
	}

	res := v.fusion.Combine(metaRes, elaRes, aiRes, freqRes)
	res.Metadata = precheck
	res.Metadata["plan"] = plan
	res.Metadata["profile"] = profile
	return res, nil
}

func (v *Verifier) metadata(ctx context.Context, sha string, img []byte) (*model.MetadataResult, error) {
	key := storage.StageCacheKey("metadata", sha, "v1", "default")
	var cached model.MetadataResult
	if v.redis != nil {
		if ok, err := v.redis.GetJSON(ctx, key, &cached); err == nil && ok {
			return &cached, nil
		}
	}
	res, err := v.meta.Analyze(img)
	if err == nil && v.redis != nil {
		_ = v.redis.SetJSON(ctx, key, res, v.stageTTL)
	}
	return res, err
}

func (v *Verifier) elaResult(ctx context.Context, sha string, img []byte) (*model.ELAResult, error) {
	key := storage.StageCacheKey("ela", sha, "v1", "threshold_0.06")
	var cached model.ELAResult
	if v.redis != nil {
		if ok, err := v.redis.GetJSON(ctx, key, &cached); err == nil && ok {
			return &cached, nil
		}
	}
	res, err := v.ela.Analyze(img)
	if err == nil && v.redis != nil {
		cacheCopy := *res
		cacheCopy.HeatmapPNG = nil
		_ = v.redis.SetJSON(ctx, key, &cacheCopy, v.stageTTL)
	}
	return res, err
}

func (v *Verifier) frequency(ctx context.Context, sha string, img []byte) (*model.FrequencyResult, error) {
	key := storage.StageCacheKey("frequency", sha, "v1", "n256")
	var cached model.FrequencyResult
	if v.redis != nil {
		if ok, err := v.redis.GetJSON(ctx, key, &cached); err == nil && ok {
			return &cached, nil
		}
	}
	res, err := v.freq.Analyze(img)
	if err == nil && v.redis != nil {
		_ = v.redis.SetJSON(ctx, key, res, v.stageTTL)
	}
	return res, err
}

func (v *Verifier) aiResult(ctx context.Context, sha string, img []byte) (*model.AIResult, error) {
	key := storage.StageCacheKey("ai", sha, "gan_detector_v1.2.0", "224")
	var cached model.AIResult
	if v.redis != nil {
		if ok, err := v.redis.GetJSON(ctx, key, &cached); err == nil && ok {
			return &cached, nil
		}
	}
	res, err := v.ai.Detect(img)
	if err == nil && v.redis != nil {
		_ = v.redis.SetJSON(ctx, key, res, v.stageTTL)
	}
	return res, err
}

func (v *Verifier) cachedResult(ctx context.Context, tenantID, sha, profile string) *model.VerificationResult {
	if v.redis == nil {
		return nil
	}
	var res model.VerificationResult
	if ok, err := v.redis.GetJSON(ctx, storage.ResultCacheKey(tenantID, sha, profile), &res); err == nil && ok {
		return &res
	}
	return nil
}

func (v *Verifier) cacheResult(ctx context.Context, tenantID, sha, profile string, res *model.VerificationResult) {
	if v.redis == nil || res == nil {
		return
	}
	_ = v.redis.SetJSON(ctx, storage.ResultCacheKey(tenantID, sha, profile), res, v.resultTTL)
}

func imageSHA256(img []byte) string {
	sum := sha256.Sum256(img)
	return hex.EncodeToString(sum[:])
}

func originalKey(tenantID, sha string) string {
	return "originals/" + tenantID + "/" + sha + ".jpg"
}

func imagePrecheck(img []byte) map[string]interface{} {
	meta := map[string]interface{}{"bytes": len(img)}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil {
		meta["decode_error"] = err.Error()
		return meta
	}
	meta["mime"] = "image/" + format
	meta["width"] = cfg.Width
	meta["height"] = cfg.Height
	return meta
}

func heatmapKey(plan, tenantID, verificationID string) string {
	switch NormalizePlan(plan) {
	case PlanFree:
		return "heatmaps/free/" + tenantID + "/" + verificationID + ".png"
	case PlanStarter:
		return "heatmaps/starter/" + tenantID + "/" + verificationID + ".png"
	case PlanPro, PlanBusiness:
		return "heatmaps/pro/" + tenantID + "/" + verificationID + ".png"
	default:
		return "heatmaps/enterprise/" + tenantID + "/" + verificationID + ".png"
	}
}
