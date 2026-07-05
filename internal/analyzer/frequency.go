package analyzer

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"

	"gonum.org/v1/gonum/dsp/fourier"

	"github.com/verifood/verifood/internal/model"
)

// FrequencyAnalyzer aplica FFT 2D e mede o desbalanceamento entre bandas
// alta e baixa de frequência. Imagens generativas costumam apresentar
// desbalanceamento fora do intervalo [0.5, 2.0].
type FrequencyAnalyzer struct{}

// NewFrequencyAnalyzer constrói o analisador espectral.
func NewFrequencyAnalyzer() *FrequencyAnalyzer { return &FrequencyAnalyzer{} }

// Analyze converte a imagem para grayscale, aplica FFT por linha (proxy 1D
// suficiente para computar razão high/low) e devolve o veredito.
func (f *FrequencyAnalyzer) Analyze(imgBytes []byte) (*model.FrequencyResult, error) {
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return &model.FrequencyResult{Confidence: 0.2}, nil
	}

	// Downsample para performance
	const N = 256
	gray := resizeGray(img, N, N)

	fft := fourier.NewFFT(N)
	var highEnergy, lowEnergy float64

	for y := 0; y < N; y++ {
		row := make([]float64, N)
		for x := 0; x < N; x++ {
			row[x] = float64(gray[y*N+x]) / 255.0
		}
		coeffs := fft.Coefficients(nil, row)
		for k, c := range coeffs {
			mag := real(c)*real(c) + imag(c)*imag(c)
			if k < N/8 {
				lowEnergy += mag
			} else if k > N/2-N/8 {
				highEnergy += mag
			}
		}
	}

	if lowEnergy == 0 {
		lowEnergy = 1e-9
	}
	imbalance := highEnergy / lowEnergy
	suspicious := imbalance > 2.0 || imbalance < 0.5

	// Confidence: distância normalizada de 1.0 (equilíbrio ideal)
	dist := math.Abs(math.Log10(imbalance))
	confidence := math.Min(dist/1.5, 1.0)

	return &model.FrequencyResult{
		IsSuspicious:  suspicious,
		Confidence:    confidence,
		HighFreqRatio: highEnergy,
		LowFreqRatio:  lowEnergy,
	}, nil
}

func resizeGray(img image.Image, w, h int) []uint8 {
	b := img.Bounds()
	sx := float64(b.Dx()) / float64(w)
	sy := float64(b.Dy()) / float64(h)
	out := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ox := b.Min.X + int(float64(x)*sx)
			oy := b.Min.Y + int(float64(y)*sy)
			r, g, bl, _ := img.At(ox, oy).RGBA()
			// Luminância BT.601
			lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)) / 257.0
			out[y*w+x] = uint8(lum)
		}
	}
	return out
}
