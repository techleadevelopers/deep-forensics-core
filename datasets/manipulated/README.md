# Dataset — Imagens Manipuladas (`manipulated/`)

Este diretório contém imagens editadas com ferramentas tradicionais ou parcialmente
alteradas com IA generativa. São a classe positiva para o detector de fraudes.

## Labels e categorias

| Label        | `category`                  | Descrição                                                  |
|--------------|-----------------------------|------------------------------------------------------------|
| `manipulated`| `photoshop_edit`            | Editada no Photoshop (clone stamp, generative fill, etc.)  |
| `manipulated`| `gimp_edit`                 | Editada no GIMP                                            |
| `manipulated`| `lightroom_edit`            | Ajustes destrutivos no Lightroom (HSL extremo, mascaras)   |
| `manipulated`| `multi_recompressed`        | Editada + múltiplas recompressões JPEG para mascarar ELA   |
| `partial`    | `partial_inpainting`        | Região parcial (~20–40%) alterada via inpainting de SD     |
| `partial`    | `partial_generative_fill`   | Photoshop Generative Fill em parte da imagem               |

## Técnicas de manipulação a representar

1. **Clone stamp** — Copiar região de outra foto e colar sobre alimento
2. **Hue/Saturation extremo** — Simular deterioração por mudança de cor
3. **Compositing** — Inserir inseto/objeto estranho de outra imagem
4. **Inpainting AI** — Preencher região com conteúdo sintético (SD, Firefly)
5. **Multi-recompressão pós-edit** — Salvar 3× em JPEG q=70 após edição
6. **Resize + reedit** — Redimensionar antes de editar (altera ELA fingerprint)

## Como popular

1. Parta de imagens autênticas do diretório `real/`.
2. Aplique as edições usando a ferramenta listada.
3. Salve em JPEG (não use PNG — os clientes submetem JPEG).
4. Registre o método nas `notes` do manifest:
   ```json
   {"path":"manipulated/sample_photoshop_001.jpg","label":"manipulated",
    "category":"photoshop_edit","notes":"clone stamp 15% área, inseto inserido, q=85"}
   ```

## Volume recomendado

- Mínimo: **50 imagens** por categoria.
- Ideal: paridade com o conjunto `real/` (evitar desbalanceamento de classes).
