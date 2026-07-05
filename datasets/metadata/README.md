# Metadata do Dataset (`metadata/`)

Este diretório armazena arquivos de metadados auxiliares sobre o dataset de avaliação.

## Arquivos esperados

| Arquivo                      | Descrição                                                      |
|------------------------------|----------------------------------------------------------------|
| `sources.json`               | Origem de cada imagem (câmera, app, modelo, prompt)            |
| `acquisition_log.jsonl`      | Log de aquisição (data, operador, método)                      |
| `exclusions.txt`             | Hashes SHA256 de imagens excluídas por problemas de qualidade  |
| `class_weights.json`         | Pesos de classe para treino (útil se dataset desbalanceado)    |

## sources.json — formato

```json
{
  "real/sample_camera_001.jpg": {
    "device": "iPhone 14 Pro",
    "app": null,
    "date_acquired": "2026-07-01",
    "license": "proprietary",
    "operator": "equipe-verifood"
  },
  "ai_generated/sample_sd_001.jpg": {
    "model": "stable-diffusion-xl-base-1.0",
    "prompt": "moldy food, rotten meal",
    "seed": 42,
    "steps": 30,
    "cfg_scale": 7.5,
    "date_acquired": "2026-07-01"
  }
}
```

## class_weights.json — formato

Usado para lidar com desbalanceamento de classes:
```json
{
  "authentic":    1.0,
  "manipulated":  2.5,
  "ai_generated": 2.0,
  "partial":      3.0
}
```

## acquisition_log.jsonl — formato

```json
{"timestamp":"2026-07-01T10:00:00Z","operator":"alice","action":"add","path":"real/sample_camera_001.jpg","label":"authentic","notes":"adicionada ao dataset v1.0"}
```
