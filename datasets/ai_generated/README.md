# Dataset — Imagens Geradas por IA (`ai_generated/`)

Este diretório contém imagens criadas integralmente por modelos de IA generativa.
Label sempre `ai_generated`.

## Categorias e ferramentas

| `category`              | Ferramenta                    | Notas                                          |
|-------------------------|-------------------------------|------------------------------------------------|
| `ai_stable_diffusion`   | Stable Diffusion XL / 1.5     | txt2img e img2img                              |
| `ai_midjourney`         | Midjourney v5 / v6            | Prompt direto                                  |
| `ai_dalle`              | DALL-E 3 (API OpenAI)         | Via API                                        |
| `ai_flux`               | Flux.1 (Black Forest Labs)    | Alta resolução                                 |
| `ai_img2img`            | Qualquer modelo, img2img      | Foto real transformada → parece real mas é IA |

## Prompts sugeridos para geração

Use prompts voltados a alimentos (contexto do VeriFood):

```
food photography, restaurant dish, professional lighting, RAW photo,
high quality, realistic --ar 4:3
```

Para simular fraudes realistas:
```
moldy food, rotten meal, insects in food, raw meat disguised as cooked,
realistic photo, DSLR camera, food delivery photo
```

## Como adicionar

1. Gere as imagens nas ferramentas acima.
2. Salve em JPEG (converta se necessário: `convert image.png -quality 92 image.jpg`).
3. Registre no manifest:
   ```json
   {"path":"ai_generated/sample_sd_001.jpg","label":"ai_generated",
    "category":"ai_stable_diffusion","notes":"SDXL 1.0, txt2img, prompt: food photography rotten"}
   ```

## Volume recomendado

- Mínimo: **30 imagens** por categoria (6 categorias = ~180 total).
- Diversificar prompts e seeds para evitar artefatos repetitivos.

## Importante

Imagens `ai_img2img` são as mais difíceis de detectar (preservam estrutura original).
Inclua pelo menos 20 exemplos desta categoria para testar os limites do detector.
