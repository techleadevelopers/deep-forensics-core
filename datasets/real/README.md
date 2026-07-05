# Dataset — Imagens Autênticas (`real/`)

Este diretório contém imagens originais de câmera, **sem manipulação**, usadas como
classe negativa (autêntica) no evaluation harness.

## Categorias esperadas

| Arquivo pattern        | `category` no manifest      | Descrição                                        |
|------------------------|-----------------------------|--------------------------------------------------|
| `sample_camera_*.jpg`  | `camera_original`           | Foto direta de câmera, EXIF intacto              |
| `sample_whatsapp_*.jpg`| `whatsapp_compressed`       | Foto autêntica após compressão do WhatsApp       |
| `sample_instagram_*.jpg`| `instagram_compressed`     | Foto autêntica após compressão do Instagram      |
| `sample_exif_stripped_*.jpg`| `exif_stripped`        | Foto autêntica com EXIF removido via exiftool    |
| `sample_resized_*.jpg` | `resized`                   | Foto autêntica redimensionada                    |
| `sample_multicomp_*.jpg`| `multi_recompressed`       | Foto com 2–5 recompressões JPEG consecutivas     |
| `sample_screenshot_*.jpg`| `screenshot`              | Screenshot de foto autêntica (sem edição)        |

## Como popular

1. Colete fotos originais de câmera (JPEG, PNG).
2. Para gerar variantes de compressão:
   ```bash
   # WhatsApp-like (qualidade ~55):
   ffmpeg -i original.jpg -q:v 10 sample_whatsapp_001.jpg

   # Instagram-like (qualidade ~70, max 1080px):
   ffmpeg -i original.jpg -vf scale=-1:1080 -q:v 5 sample_instagram_001.jpg

   # Remover EXIF:
   exiftool -all= -o sample_exif_stripped_001.jpg original.jpg

   # Multi-recompressão:
   for i in 1 2 3; do
     cp current.jpg tmp.jpg
     ffmpeg -i tmp.jpg -q:v 8 current.jpg
   done
   ```

3. Adicione uma linha no `manifest.jsonl` para cada arquivo:
   ```json
   {"path":"real/sample_camera_001.jpg","label":"authentic","category":"camera_original","notes":"iPhone 14 Pro, sem edição"}
   ```

## Volume recomendado

- Mínimo: **50 imagens** por categoria para resultados confiáveis.
- Ideal: **200+ imagens** com diversidade de dispositivos, condições de luz e tipos de alimento.

## Direitos autorais

Use apenas imagens próprias ou sob licença livre (CC0, Unsplash, Pexels).
Não comitar imagens de clientes ou com dados pessoais.
