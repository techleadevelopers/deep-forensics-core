# PixelAudit — Backend (Go)

API forense em Go + workers ONNX. Zero Python.

## Estrutura

```
backend/
├── cmd/
│   ├── api/         # Servidor HTTP Gin
│   └── worker/      # Consumidor NATS JetStream
├── internal/
│   ├── analyzer/    # metadata.go, ela.go, ai.go, frequency.go, fusion.go
│   ├── api/         # Handlers, middlewares (auth, rate limit, logging)
│   ├── config/      # Loader de env
│   ├── model/       # DTOs compartilhados
│   ├── orchestrator/# Fan-out das 4 análises
│   ├── queue/       # NATS wrapper
│   └── storage/     # Postgres, Redis, S3
├── migrations/      # SQL inicial
├── models/          # Modelos ONNX (não versionados)
├── Dockerfile
├── docker-compose.yml
└── go.mod
```

## Subir tudo local

```bash
cd backend
docker compose up --build
```

- API      : http://localhost:8080
- MinIO UI : http://localhost:9001 (minioadmin / minioadmin)
- NATS mon : http://localhost:8222

## Verificar uma imagem

```bash
curl -X POST http://localhost:8080/v1/verify \
  -H "Authorization: Bearer sk_live_test1234" \
  -F "image=@sample.jpg" \
  -F "order_id=ORD-1"
```

Resposta em ≤500ms (sync) ou `202 Accepted` (com `webhook_url`).

## Rodar sem Docker

```bash
go mod tidy
export DATABASE_URL=postgres://PixelAudit:PixelAudit@localhost:5432/PixelAudit?sslmode=disable
export REDIS_URL=redis://localhost:6379/0
export NATS_URL=nats://localhost:4222
export S3_ENDPOINT=http://localhost:9000
export S3_ACCESS_KEY=minioadmin S3_SECRET_KEY=minioadmin
export JWT_SECRET=dev
go run ./cmd/api    # em uma aba
go run ./cmd/worker # em outra
```

## Camadas de análise

| Arquivo | Peso | Descrição |
|---------|------|-----------|
| `analyzer/metadata.go` | 0.25 | EXIF/XMP + blacklist Photoshop/Midjourney/DALL-E |
| `analyzer/ela.go`      | 0.25 | Recompressão JPEG q=90, heatmap PNG |
| `analyzer/ai.go`       | 0.35 | Inferência ONNX (CNN 224×224) |
| `analyzer/frequency.go`| 0.15 | FFT 2D, razão high/low freq |
| `analyzer/fusion.go`   |  —   | Score ponderado + recomendação ACCEPT/REVIEW/REJECT |

## Notas

- O modelo ONNX real (`models/ai_detector_v1.2.0.onnx`) deve ser adicionado à imagem antes do deploy. O código detecta ausência e desativa a camada AI gracefulmente.
- Auth atual é placeholder (Bearer sem lookup DB). Implementar `bcrypt` + tabela `tenants.api_key_hash` antes de produção.
- Ver [ARCHITECTURE.md](../ARCHITECTURE.md) na raiz do repo para SLOs, threat model e capacity planning.
