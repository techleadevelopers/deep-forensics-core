package analyzer

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	"github.com/verifood/verifood/internal/model"
)

// ELAAnalyzer implementa Error Level Analysis: recomprime a imagem em JPEG q=90
// e mede a diferença pixel-a-pixel. Regiões editadas apresentam energia maior.
type ELAAnalyzer struct {
	Threshold float64 // energia média acima da qual consideramos "editado"
}

// NewELAAnalyzer constrói o analisador com o threshold desejado.
func NewELAAnalyzer(threshold float64) *ELAAnalyzer {
	return &ELAAnalyzer{Threshold: threshold}
}

// Analyze decodifica a imagem, recomprime, computa o diff visual e gera heatmap PNG.
func (e *ELAAnalyzer) Analyze(imgBytes []byte) (*model.ELAResult, error) {
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return &model.ELAResult{Confidence: 0.2}, nil
	}

	// Recompressão JPEG q=90
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		return &model.ELAResult{Confidence: 0.2}, nil
	}
	recomp, err := jpeg.Decode(&buf)
	if err != nil {
		return &model.ELAResult{Confidence: 0.2}, nil
	}

	bounds := img.Bounds()
	heat := image.NewGray(bounds)
	var total, maxDiff float64
	var count float64

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r1, g1, b1, _ := img.At(x, y).RGBA()
			r2, g2, b2, _ := recomp.At(x, y).RGBA()
			dr := absDiff(r1, r2) / 65535.0
			dg := absDiff(g1, g2) / 65535.0
			db := absDiff(b1, b2) / 65535.0
			d := (dr + dg + db) / 3.0
			if d > maxDiff {
				maxDiff = d
			}
			total += d
			count++
			// Amplifica ×10 para visualização
			v := d * 2550.0
			if v > 255 {
				v = 255
			}
			heat.SetGray(x, y, color.Gray{Y: uint8(v)})
		}
	}

	avg := total / count
	hasEdits := avg > e.Threshold

	// PNG do heatmap
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, heat)

	// Confidence normalizada: 0..1
	confidence := avg / (e.Threshold * 4)
	if confidence > 1 {
		confidence = 1
	}

	return &model.ELAResult{
		HasEdits:       hasEdits,
		EditPercentage: avg * 100,
		Confidence:     confidence,
		HeatmapPNG:     pngBuf.Bytes(),
	}, nil
}

func absDiff(a, b uint32) float64 {
	if a > b {
		return float64(a - b)
	}
	return float64(b - a)
}
