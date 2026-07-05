# PixelAudit — Architecture Document

> Documento de arquitetura técnica de referência. Nível: **detalhado / engenharia**.
> Última revisão: 2026-07-05. Owner: Platform Engineering.

---

## Sumário

1. [Visão Geral e Princípios](#1-visão-geral-e-princípios)
2. [Requisitos Não-Funcionais (SLOs)](#2-requisitos-não-funcionais-slos)
3. [Modelo C4](#3-modelo-c4)
4. [Componentes de Domínio](#4-componentes-de-domínio)
5. [Pipeline Forense](#5-pipeline-forense)
6. [Modelos de Machine Learning](#6-modelos-de-machine-learning)
7. [Modelo de Dados](#7-modelo-de-dados)
8. [APIs e Contratos](#8-apis-e-contratos)
9. [Concorrência e Fluxo Assíncrono](#9-concorrência-e-fluxo-assíncrono)
10. [Deploy e Infraestrutura](#10-deploy-e-infraestrutura)
11. [Observabilidade](#11-observabilidade)
12. [Segurança e Threat Model](#12-segurança-e-threat-model)
13. [Estratégia de Testes](#13-estratégia-de-testes)
14. [Escalabilidade e Capacity Planning](#14-escalabilidade-e-capacity-planning)
15. [Decisões Arquiteturais (ADRs)](#15-decisões-arquiteturais-adrs)

---

## 1. Visão Geral e Princípios

PixelAudit é uma **plataforma de verificação forense de imagens como serviço**, projetada para:

- **Latência baixa e previsível** (p95 < 500ms) mesmo sob carga.
- **Auditabilidade total**: toda decisão precisa ser reproduzível e explicável (heatmaps, scores parciais, versões de modelo).
- **Custo unitário mínimo**: análise inteira roda em **CPU**, sem GPU obrigatória.
- **Isolamento tenant**: multi-tenancy com quotas, rate limits e criptografia por chave de cliente.

### Princípios arquiteturais

| Princípio | Aplicação |
|-----------|-----------|
| **Fail-fast, fail-loud** | Validação sincrônica no gateway; erros semânticos em ≤50ms |
| **Idempotência** | Toda mutação chave (verify, webhook) usa `Idempotency-Key` |
| **Stateless workers** | Workers não retêm estado; escalam horizontalmente por queue depth |
| **Backpressure explícito** | NATS JetStream com `MaxAckPending`; rejeição controlada acima do limite |
| **Zero-trust interno** | mTLS entre serviços, RBAC por service account |
| **Deterministic ML** | Modelos versionados imutáveis; toda inferência grava `model_id + hash` |

---

## 2. Requisitos Não-Funcionais (SLOs)

| Métrica | Target | Janela |
|---------|--------|--------|
| Disponibilidade API | 99.95% | 30 dias |
| Latência `/verify` (p50) | < 180ms | 5 min |
| Latência `/verify` (p95) | < 500ms | 5 min |
| Latência `/verify` (p99) | < 1200ms | 5 min |
| Throughput sustentado | 500 req/s por região | contínuo |
| Taxa de erro 5xx | < 0.1% | 1h |
| Precisão de detecção de IA (F1) | ≥ 0.92 | benchmark trimestral |
| False positive rate | < 3% | benchmark trimestral |
| RTO (recovery time) | 15 min | evento único |
| RPO (data loss) | 30s | evento único |

---

## 3. Modelo C4

### 3.1 Nível 1 — Contexto

```text
┌────────────────────────────────────────────────────────────────┐
│                     Ecossistema PixelAudit                       │
│                                                                │
│  ┌──────────┐   REST/    ┌────────────┐   Webhooks  ┌────────┐ │
│  │ Cliente  │──HTTPS────▶│  PixelAudit  │────────────▶│Sistema │ │
│  │ (iFood,  │            │  Platform  │             │Cliente │ │
│  │  Uber…)  │◀───────────│            │             │(CRM…)  │ │
│  └──────────┘  response  └─────┬──────┘             └────────┘ │
│                                │                                │
│                                ▼                                │
│                       ┌─────────────────┐                       │
│                       │  Model Registry │  (S3 + DVC)           │
│                       │  + Training     │                       │
│                       │  Pipeline       │                       │
│                       └─────────────────┘                       │
└────────────────────────────────────────────────────────────────┘
```

### 3.2 Nível 2 — Containers

```text
┌───────────────────────────────────────────────────────────────────┐
│                        PixelAudit Platform                          │
│                                                                   │
│   ┌───────────────┐    ┌────────────────┐    ┌────────────────┐   │
│   │ API Gateway   │───▶│ Verification   │───▶│ Score Fusion   │   │
│   │ (Gin, Go)     │    │ Orchestrator   │    │ Service (Go)   │   │
│   └───────┬───────┘    │ (Go)           │    └───────┬────────┘   │
│           │            └────────┬───────┘            │            │
│           │                     │                    │            │
│           ▼                     ▼                    ▼            │
│   ┌───────────────┐    ┌────────────────┐    ┌────────────────┐   │
│   │ Auth/Rate     │    │ NATS JetStream │    │ Result Store   │   │
│   │ (Redis)       │    │ (queue)        │    │ (Postgres+S3)  │   │
│   └───────────────┘    └────────┬───────┘    └────────────────┘   │
│                                 │                                 │
│         ┌───────────────────────┼────────────────────┐            │
│         ▼                       ▼                    ▼            │
│   ┌──────────┐          ┌──────────────┐      ┌──────────────┐    │
│   │ Metadata │          │ ELA + Freq   │      │ AI Detector  │    │
│   │ Worker   │          │ Worker       │      │ Worker (ONNX)│    │
│   └──────────┘          └──────────────┘      └──────────────┘    │
│                                                                   │
│   ┌───────────────┐    ┌────────────────┐    ┌────────────────┐   │
│   │ Webhook       │    │ Admin Dashboard│    │ Billing/Usage  │   │
│   │ Dispatcher    │    │ (React + TSS)  │    │ Aggregator     │   │
│   └───────────────┘    └────────────────┘    └────────────────┘   │
└───────────────────────────────────────────────────────────────────┘
```

### 3.3 Nível 3 — Componentes internos do Verification Orchestrator

```text
┌─────────────────────────────────────────────────────────┐
│              Verification Orchestrator                   │
│                                                          │
│   ┌────────────┐   ┌───────────┐   ┌───────────────┐    │
│   │ Ingestor   │──▶│ Validator │──▶│ Deduplicator  │    │
│   │ (multipart │   │ (MIME,    │   │ (perceptual   │    │
│   │  + base64) │   │  size,    │   │  hash pHash)  │    │
│   └────────────┘   │  format)  │   └───────┬───────┘    │
│                    └───────────┘           │            │
│                                            ▼            │
│                                   ┌────────────────┐    │
│                                   │ Fan-out        │    │
│                                   │ (errgroup +    │    │
│                                   │  NATS publish) │    │
│                                   └────────┬───────┘    │
│                                            │            │
│                                            ▼            │
│                                   ┌────────────────┐    │
│                                   │ Result Collector│   │
│                                   │ (timeout=800ms)│    │
│                                   └────────────────┘    │
└─────────────────────────────────────────────────────────┘
```

---

## 4. Componentes de Domínio

### 4.1 API Gateway (`cmd/api`)

- Framework: **Gin** (`github.com/gin-gonic/gin`).
- Middlewares: `RequestID`, `OTELTracing`, `Auth (JWT/HMAC)`, `TenantContext`, `RateLimit`, `IdempotencyKey`, `RequestBodyLimit(25MB)`.
- Handlers: `POST /v1/verify`, `GET /v1/verify/:id`, `GET /v1/verify/:id/heatmap`, `POST /v1/webhooks/test`, `GET /v1/health`, `GET /v1/metrics`.
- Response envelope: `{ id, status, data?, error? }`.
- Timeout global: `context.WithTimeout(ctx, 2s)`.

### 4.2 Verification Orchestrator (`internal/orchestrator`)

- Recebe imagem validada, calcula `sha256 + pHash`.
- Persiste imagem original em S3 (`bucket/{tenant}/{yyyy-mm-dd}/{sha256}.{ext}`).
- Publica evento `verification.requested` em NATS JetStream com payload:
  ```json
  {
    "verification_id": "ver_...",
    "tenant_id": "tnt_...",
    "s3_key": "...",
    "sha256": "...",
    "requested_at": "..."
  }
  ```
- Aguarda resultados dos workers via **Redis Pub/Sub** ou **NATS request-reply** com timeout de 800ms.
- Se timeout, retorna `202 Accepted` com `id` e permite polling / webhook.

### 4.3 Workers (`cmd/worker-*`)

Cada worker é um binário independente, consumidor de um subject NATS específico:

| Worker | Subject NATS | Consumer group | Réplicas base |
|--------|--------------|----------------|---------------|
| Metadata | `verify.metadata` | `metadata-cg` | 2 |
| ELA + Frequência | `verify.pixel` | `pixel-cg` | 4 |
| AI (ONNX) | `verify.ai` | `ai-cg` | 6 |
| Score Fusion | `verify.fusion` | `fusion-cg` | 2 |
| Webhook Dispatcher | `webhook.dispatch` | `webhook-cg` | 2 |

Padrão: `MaxAckPending=100`, `AckWait=30s`, `MaxDeliver=3`, DLQ em `verify.<name>.dlq`.

### 4.4 Score Fusion Service

- Consome os 4 sub-scores + regras de negócio configuráveis por tenant.
- Fórmula base ponderada + **override rules** (ex: `if metadata.software contains "Midjourney" ⇒ force REJECT`).
- Persiste resultado final em `verifications` (Postgres) e enfileira webhook.

### 4.5 Webhook Dispatcher

- Assinatura HMAC-SHA256 do payload (`X-PixelAudit-Signature: t=<ts>,v1=<sig>`).
- Retry exponencial: `[5s, 30s, 2m, 10m, 1h, 6h, 24h]` (7 tentativas) via delayed queue.
- Dead letter em `webhooks_failed` após 7 falhas; alerta operacional.

---

## 5. Pipeline Forense

### 5.1 Metadata Analyzer

- Bibliotecas: `dsoprea/go-exif/v3` (parsing puro Go), fallback para `exiftool` binary via `barasher/go-exiftool` para XMP/IPTC exóticos.
- Extrai: EXIF, XMP, IPTC, MakerNotes, ICC profile.
- Heurísticas de suspeita:
  - Software conhecido de edição/IA (blacklist versionada).
  - `DateTimeOriginal != DateTimeDigitized`.
  - Ausência de tags críticas em fotos que alegam ser de câmera.
  - `Software` contendo strings de modelos generativos (`Stable Diffusion`, `Midjourney`, `DALL-E`, `Firefly`).
  - Presença de campo `C2PA` inválido ou removido.
- Output: `confidence ∈ [0,1]`.

### 5.2 Error Level Analysis (ELA)

- Recomprime a imagem em JPEG q=90.
- Calcula diferença absoluta por canal, produz heatmap `image.Gray`.
- Métrica: `edit_ratio = pixels_above_threshold / total_pixels` (threshold=15/255).
- Salva heatmap PNG em S3, retorna URL assinada (expira em 24h).

### 5.3 AI Detector (ONNX)

- Modelo base: **EfficientNet-B0** fine-tuned em ~1.2M imagens (500k reais + 700k geradas de SD, MJ, DALL-E, SDXL, Flux).
- Pré-processamento em Go: resize bilinear 224×224, normalização mean/std ImageNet.
- Runtime: `github.com/yalue/onnxruntime_go`, sessão reutilizada por worker (thread-safe).
- Warmup: 10 inferências dummy no boot.
- Latência-alvo: ≤ 60ms em vCPU x86_64 (AVX2).

### 5.4 Frequency Analyzer

- Converte para grayscale, aplica **FFT 2D** (`gonum/dsp/fourier`).
- Segmenta espectro em bandas radial (low/mid/high).
- Métrica: `imbalance = high_energy / low_energy`.
- Imagens generativas tendem a apresentar `imbalance` fora do intervalo `[0.5, 2.0]` — output = distância normalizada do centro.

### 5.5 Score Fusion

```go
type Weights struct {
    Metadata  float64 // default 0.25
    ELA       float64 // default 0.25
    AI        float64 // default 0.35
    Frequency float64 // default 0.15
}

// Configurável por tenant via feature flags (LaunchDarkly / Unleash).
final := m.Confidence*w.Metadata +
         e.Confidence*w.ELA +
         a.Confidence*w.AI +
         f.Confidence*w.Frequency

recommendation := switch {
  final >= 0.75: REJECT
  final >= 0.45: REVIEW
  default:       ACCEPT
}
```

---

## 6. Modelos de Machine Learning

### 6.1 Ciclo de vida

```text
Data Collection ──▶ Labeling ──▶ Training (PyTorch)
       ▲                              │
       │                              ▼
   Feedback loop ◀── Prod Metrics ── Export ONNX ──▶ Model Registry (S3+DVC)
                                                          │
                                                          ▼
                                                    Canary deploy (5%)
                                                          │
                                                          ▼
                                                    Full rollout
```

### 6.2 Governança

- Cada modelo é versionado (`v<major>.<minor>.<patch>`).
- Metadata obrigatória: dataset hash, hyperparams, métricas de validação (accuracy, F1, ROC-AUC, per-generator breakdown).
- **Model card** publicado em `/models/<version>/CARD.md`.
- Rollback via feature flag `ai_model_version` (default = stable, canary = candidate).

### 6.3 Retraining

- Trigger: drift detectado (Population Stability Index > 0.2) OU semanal cronograma.
- Pipeline: **Kubeflow Pipelines** em cluster separado (não afeta produção).
- Aprovação humana obrigatória antes de promover `candidate → stable`.

---

## 7. Modelo de Dados

### 7.1 Schema Postgres (simplificado)

```sql
CREATE TABLE tenants (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    plan          TEXT NOT NULL CHECK (plan IN ('free','starter','pro','enterprise')),
    api_key_hash  BYTEA NOT NULL,
    weights       JSONB NOT NULL DEFAULT '{}',
    rate_limit_rpm INT NOT NULL DEFAULT 60,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE verifications (
    id                TEXT PRIMARY KEY, -- ULID
    tenant_id         UUID NOT NULL REFERENCES tenants(id),
    sha256            BYTEA NOT NULL,
    phash             BIGINT NOT NULL,
    s3_key            TEXT NOT NULL,
    heatmap_s3_key    TEXT,
    status            TEXT NOT NULL CHECK (status IN ('pending','completed','failed')),
    authentic         BOOLEAN,
    confidence        NUMERIC(5,2),
    recommendation    TEXT,
    priority          TEXT,
    analysis          JSONB, -- resultados detalhados
    model_versions    JSONB, -- { "ai": "v1.2.0", ... }
    processing_time_ms INT,
    order_id          TEXT,
    metadata          JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at      TIMESTAMPTZ
);
CREATE INDEX idx_verif_tenant_created ON verifications(tenant_id, created_at DESC);
CREATE INDEX idx_verif_phash ON verifications USING hash(phash);
CREATE INDEX idx_verif_sha256 ON verifications(sha256);

CREATE TABLE webhooks (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    verification_id TEXT REFERENCES verifications(id) ON DELETE CASCADE,
    url            TEXT NOT NULL,
    status         TEXT NOT NULL, -- pending, delivered, failed
    attempts       INT NOT NULL DEFAULT 0,
    last_attempt   TIMESTAMPTZ,
    next_attempt   TIMESTAMPTZ,
    response_code  INT,
    response_body  TEXT
);

CREATE TABLE usage_events (
    tenant_id    UUID NOT NULL,
    day          DATE NOT NULL,
    count        BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, day)
);
```

### 7.2 Estratégia de particionamento

- `verifications` particionada por `RANGE (created_at)` mensalmente.
- Retenção padrão: 90 dias hot, 12 meses cold (S3 Glacier).
- LGPD: purge automático via job semanal respeitando flag `retain_evidence`.

### 7.3 Redis (data structures)

| Key pattern | Tipo | TTL | Uso |
|-------------|------|-----|-----|
| `rl:{tenant}:{minute}` | INCR + EXPIRE | 60s | Rate limit sliding |
| `idem:{tenant}:{key}` | STRING | 24h | Idempotência |
| `dedup:{phash}` | STRING (ver_id) | 5min | Dedup rápida |
| `result:{ver_id}` | HASH | 10min | Resultado transitório |

---

## 8. APIs e Contratos

### 8.1 OpenAPI

Especificação em `/api/openapi.yaml` (OpenAPI 3.1). Geração automática de SDKs via **openapi-generator** no CI.

### 8.2 Versionamento

- Prefixo `/v1`, `/v2` em rota.
- Breaking changes exigem nova major; deprecação com 6 meses de aviso via header `Sunset`.

### 8.3 Rate limiting

- Sliding window por tenant.
- Headers de resposta: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`.

### 8.4 Erros

Formato padronizado (RFC 7807 Problem Details):

```json
{
  "type": "https://PixelAudit.io/errors/invalid-image",
  "title": "Invalid image",
  "status": 400,
  "detail": "Image MIME type application/pdf not supported",
  "instance": "/v1/verify",
  "request_id": "req_..."
}
```

---

## 9. Concorrência e Fluxo Assíncrono

### 9.1 Padrão fan-out/fan-in

```go
func (o *Orchestrator) Verify(ctx context.Context, img []byte) (*Result, error) {
    ctx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
    defer cancel()

    g, gctx := errgroup.WithContext(ctx)

    var meta *MetadataResult
    var ela  *ELAResult
    var ai   *AIResult
    var freq *FreqResult

    g.Go(func() error {
        var err error
        meta, err = o.metadata.Analyze(gctx, img)
        return err
    })
    g.Go(func() error {
        var err error
        ela, err = o.ela.Analyze(gctx, img)
        return err
    })
    g.Go(func() error {
        var err error
        ai, err = o.aiDetector.Detect(gctx, img)
        return err
    })
    g.Go(func() error {
        var err error
        freq, err = o.freq.Analyze(gctx, img)
        return err
    })

    if err := g.Wait(); err != nil {
        return nil, err
    }
    return o.fusion.Combine(meta, ela, ai, freq), nil
}
```

### 9.2 Backpressure

- Workers publicam `NAK` com delay quando `MaxAckPending` alcançado.
- API responde `429 Too Many Requests` quando queue depth > threshold configurável.

### 9.3 Idempotência

- Cliente envia `Idempotency-Key: <uuid>` no header.
- Chave persistida em Redis por 24h com hash da request; reenvio retorna resposta original.

---

## 10. Deploy e Infraestrutura

### 10.1 Topologia Kubernetes

```text
Namespace: PixelAudit-prod

Deployments:
  api               (HPA: 2..20  | CPU 70%)
  orchestrator      (HPA: 2..10)
  worker-metadata   (HPA: 2..8   | queue depth)
  worker-pixel      (HPA: 4..30  | queue depth)
  worker-ai         (HPA: 6..40  | queue depth)
  worker-fusion     (HPA: 2..8)
  webhook-dispatcher(HPA: 2..8)

StatefulSets:
  postgres (Patroni HA, 3 replicas, PVC 500Gi SSD)
  redis    (Sentinel, 3 nodes)
  nats     (JetStream cluster, 3 nodes, PVC 100Gi)

Ingress:
  Traefik / NGINX + cert-manager (Let's Encrypt)

Service Mesh:
  Linkerd (mTLS automático, retries, timeouts)
```

### 10.2 CI/CD

- **CI**: GitHub Actions — lint (`golangci-lint`), test (`go test -race -cover`), build (`ko`), scan (`trivy`, `gosec`, `govulncheck`).
- **CD**: Argo CD (GitOps) monitorando repo `PixelAudit-manifests`.
- **Estratégia**: canary 10% → 50% → 100% com análise automática via Flagger (SLO error rate, latência).

### 10.3 Ambientes

| Ambiente | Cluster | Dados | Uso |
|----------|---------|-------|-----|
| dev | local (kind/minikube) | mock | desenvolvimento |
| staging | GKE us-east1 | anonimizados | QA, load test |
| prod | GKE multi-region (us-east1, sa-east1) | produção | clientes |

---

## 11. Observabilidade

### 11.1 Traces

- **OpenTelemetry SDK** em todos os serviços.
- Exporter OTLP → **Tempo** / **Jaeger**.
- Span attributes obrigatórios: `tenant.id`, `verification.id`, `model.version`, `image.sha256`.

### 11.2 Métricas

Prometheus scraping. Métricas-chave:

| Métrica | Tipo | Labels |
|---------|------|--------|
| `PixelAudit_verify_duration_seconds` | Histogram | tenant, status |
| `PixelAudit_verify_score` | Histogram | tenant, recommendation |
| `PixelAudit_worker_queue_depth` | Gauge | worker |
| `PixelAudit_ai_inference_duration_seconds` | Histogram | model_version |
| `PixelAudit_webhook_delivery_total` | Counter | status |
| `PixelAudit_ratelimit_rejections_total` | Counter | tenant |

### 11.3 Logs

- Estruturados JSON (`slog`), sink → **Loki**.
- Campos padronizados: `ts, level, msg, request_id, tenant_id, verification_id, trace_id, span_id`.

### 11.4 Alertas

- **PagerDuty** — sev1: 5xx > 1% por 5min, latência p95 > 800ms por 10min.
- **Slack** — sev2/3: drift do modelo, queue backlog > 1000, disk > 80%.

---

## 12. Segurança e Threat Model

### 12.1 Superfície de ataque

| Ativo | Ameaça | Mitigação |
|-------|--------|-----------|
| API Key | Vazamento | Hash bcrypt em DB, rotação self-service, prefix `sk_live_` para detecção secret scanners |
| Imagens | Upload malicioso (SVG/HTML/RCE via libvips CVE) | Whitelist MIME (`image/jpeg`, `image/png`, `image/webp`, `image/heic`), reparse via libvips sandbox, size cap 25MB, dimensions cap 12000×12000 |
| Metadados | XXE / injection via XMP | Parser Go puro sem entidades externas |
| Webhook secret | Man-in-the-middle | HMAC-SHA256 + timestamp com janela 5min |
| Modelo ONNX | Model theft | Cifrado em repouso (AWS KMS), servido apenas via signed URLs para workers |
| Dados de clientes | Vazamento cruzado tenant | Row-Level Security no Postgres; contexto tenant obrigatório |
| Endpoint público | DDoS | Cloudflare WAF + rate limit Redis |
| SSRF via webhook_url | Requisições internas | Bloqueio de IPs privados (RFC1918/loopback/link-local), resolver DNS em allowlist |

### 12.2 Criptografia

- Em trânsito: TLS 1.3 obrigatório (HSTS, cipher suites modernas).
- Em repouso: AES-256 (S3 SSE-KMS, disks EBS encrypted).
- Segredos: HashiCorp Vault / Kubernetes External Secrets Operator.

### 12.3 Compliance

- **LGPD** — DPO designado, DPIA por feature, direito ao esquecimento (endpoint `DELETE /v1/verifications`).
- **SOC 2 Type II** — em processo (auditor: TBD).
- **PCI DSS** — não aplicável (não armazenamos cartão).

---

## 13. Estratégia de Testes

| Nível | Ferramenta | Cobertura alvo |
|-------|-----------|----------------|
| Unit | `go test` + `testify` | ≥ 85% |
| Property-based | `gopter` | camadas críticas (fusion, metadata) |
| Integration | `testcontainers-go` (Postgres, Redis, NATS, MinIO) | fluxos ponta a ponta |
| Contract | `pact-go` | SDKs vs API |
| Load | `k6` | 1k RPS sustentado, spike 5k RPS |
| Chaos | `LitmusChaos` | kill worker, latency injection |
| ML | benchmark holdout dataset por CI | F1 ≥ threshold |

Dataset de benchmark inclui **adversarial samples**: prints de tela, screenshots de IA re-fotografados, edições sutis (< 5% pixels alterados).

---

## 14. Escalabilidade e Capacity Planning

### 14.1 Perfil de recurso por worker (baseline)

| Worker | CPU request | CPU limit | Mem request | Mem limit | Throughput |
|--------|-------------|-----------|-------------|-----------|------------|
| api | 200m | 1000m | 128Mi | 512Mi | 300 rps/pod |
| worker-metadata | 100m | 500m | 64Mi | 256Mi | 400 img/s/pod |
| worker-pixel | 500m | 2000m | 256Mi | 1Gi | 30 img/s/pod |
| worker-ai | 1000m | 4000m | 512Mi | 2Gi | 20 img/s/pod |

### 14.2 Custo estimado (produção baseline, 1M verificações/mês)

| Recurso | Custo mensal (USD) |
|---------|-------------------|
| Compute (GKE) | ~$420 |
| Postgres HA | ~$180 |
| Redis HA | ~$90 |
| NATS | ~$60 |
| S3 + egress | ~$110 |
| Observability stack | ~$150 |
| **Total** | **~$1010** |

Custo unitário: **~$0.001 por verificação** (margem >95% no plano Pro).

---

## 15. Decisões Arquiteturais (ADRs)

Registradas em `/docs/adr/`. Principais:

| ADR | Título | Status |
|-----|--------|--------|
| 0001 | Escolha de Go em vez de Python para runtime | Accepted |
| 0002 | ONNX Runtime em vez de TensorFlow Lite | Accepted |
| 0003 | NATS JetStream em vez de Kafka | Accepted |
| 0004 | Postgres JSONB em vez de MongoDB | Accepted |
| 0005 | Multi-tenant compartilhado com RLS em vez de DB per tenant | Accepted |
| 0006 | Webhook assíncrono em vez de resposta síncrona pesada | Accepted |
| 0007 | Deploy em Kubernetes em vez de Railway (após escala) | Accepted |
| 0008 | libvips em vez de ImageMagick | Accepted |
| 0009 | ULID em vez de UUID para `verification_id` | Accepted |
| 0010 | Feature flags via Unleash self-hosted | Accepted |

---

## Apêndice A — Glossário

- **ELA** — Error Level Analysis, técnica que detecta regiões editadas pela diferença entre a imagem e sua recompressão JPEG.
- **PRNU** — Photo Response Non-Uniformity, "impressão digital" única do sensor da câmera.
- **CFA** — Color Filter Array, padrão do filtro Bayer do sensor.
- **pHash** — perceptual hash, hash tolerante a pequenas variações visuais.
- **C2PA** — Coalition for Content Provenance and Authenticity, padrão de metadados de proveniência criptograficamente assinados.
- **ONNX** — Open Neural Network Exchange, formato interoperável para modelos ML.
- **DLQ** — Dead Letter Queue.
- **HPA** — Horizontal Pod Autoscaler.
- **SLO/SLI/SLA** — Service Level Objective / Indicator / Agreement.

---

*Fim do documento.*
