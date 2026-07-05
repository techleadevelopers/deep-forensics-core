package analyzer

// StatisticalAnalyzer detects AI-generated images through pixel-level
// statistical fingerprinting — no ONNX model required.
//
// Three independent signals are measured:
//
//  1. Noise Coefficient of Variation (NoiseCoV)
//     Camera sensors produce shot noise that varies spatially with texture.
//     AI-generated images have unusually uniform synthetic noise across all
//     regions. Measured as CoV of per-block Laplacian energy.
//
//  2. Inter-Channel Noise Correlation (NoiseCorrelation)
//     A camera's Bayer filter means each photosite measures only one colour.
//     Demosaicing introduces slight channel-specific patterns, making R and B
//     noise largely independent. AI synthesis generates all three channels
//     jointly — their Laplacian residuals are highly correlated.
//
//  3. Flat-Region Smoothness (FlatRegionSmooth)
//     Natural images contain photon/sensor grain even in "flat" areas
//     (clear sky, skin, walls). AI images are unnaturally smooth in those
//     regions. Measured as the per-pixel std-dev inside low-gradient zones.

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"

	"github.com/PixelAudit/PixelAudit/internal/model"
)

const statN = 256 // working resolution for statistical analysis

// StatisticalAnalyzer has no configuration — instantiate with NewStatisticalAnalyzer.
type StatisticalAnalyzer struct{}

// NewStatisticalAnalyzer returns a ready StatisticalAnalyzer.
func NewStatisticalAnalyzer() *StatisticalAnalyzer { return &StatisticalAnalyzer{} }

// Analyze runs all three statistical checks and returns a consolidated result.
func (s *StatisticalAnalyzer) Analyze(imgBytes []byte) (*model.StatisticalResult, error) {
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		// Decode failure is not an error we propagate — return neutral score.
		return &model.StatisticalResult{Confidence: 0.10}, nil
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Reject degenerate images (too small to analyse meaningfully).
	if w < 64 || h < 64 {
		return &model.StatisticalResult{Confidence: 0.10}, nil
	}

	const N = statN
	sx := float64(w) / N
	sy := float64(h) / N

	grayF := make([]float64, N*N)
	rCh := make([]float64, N*N)
	bCh := make([]float64, N*N)

	for y := 0; y < N; y++ {
		for x := 0; x < N; x++ {
			ox := bounds.Min.X + int(float64(x)*sx)
			oy := bounds.Min.Y + int(float64(y)*sy)
			r32, g32, b32, _ := img.At(ox, oy).RGBA()
			rf := float64(r32) / 65535.0
			gf := float64(g32) / 65535.0
			bf := float64(b32) / 65535.0
			// BT.601 luminance
			grayF[y*N+x] = 0.299*rf + 0.587*gf + 0.114*bf
			rCh[y*N+x] = rf
			bCh[y*N+x] = bf
		}
	}

	// ── Laplacian residuals ─────────────────────────────────────────────────
	// kernel: center −4, orthogonal neighbours +1 (border pixels = 0).
	lapGray := laplacian2D(grayF, N)
	lapR := laplacian2D(rCh, N)
	lapB := laplacian2D(bCh, N)

	// ── Signal 1: Noise Coefficient of Variation ────────────────────────────
	// Divide the Laplacian magnitude map into 16×16 blocks and compute the
	// CoV of block-level mean absolute residuals.
	noiseCoV := blockLaplacianCoV(lapGray, N, 16)

	// ── Signal 2: Inter-channel Laplacian correlation ───────────────────────
	noiseCorr := pearsonCorr64(lapR, lapB)
	if noiseCorr < 0 {
		noiseCorr = 0 // negative correlation ≡ not suspicious
	}

	// ── Signal 3: Flat-region smoothness ────────────────────────────────────
	sobelMag := sobel2D(grayF, N)
	flatStd := flatRegionStdDev(grayF, sobelMag, N)
	flatStd255 := flatStd * 255.0 // re-scale to 0-255 for reporting

	// ── Score fusion ─────────────────────────────────────────────────────────
	confidence, signals := statConfidence(noiseCoV, noiseCorr, flatStd)

	return &model.StatisticalResult{
		NoiseCoV:         noiseCoV,
		NoiseCorrelation: noiseCorr,
		FlatRegionSmooth: flatStd255,
		Confidence:       confidence,
		IsAISuspected:    confidence >= 0.50,
		Signals:          signals,
	}, nil
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// laplacian2D applies the discrete Laplacian kernel to a 1-D row-major float
// slice of dimensions N×N. Border pixels are set to 0.
func laplacian2D(px []float64, N int) []float64 {
	out := make([]float64, N*N)
	for y := 1; y < N-1; y++ {
		for x := 1; x < N-1; x++ {
			v := px[(y-1)*N+x] + px[(y+1)*N+x] +
				px[y*N+(x-1)] + px[y*N+(x+1)] -
				4*px[y*N+x]
			out[y*N+x] = v
		}
	}
	return out
}

// blockLaplacianCoV divides the Laplacian map into (N/blockSize)² blocks,
// computes the mean absolute Laplacian energy per block, then returns the
// CoV = σ/μ of those block energies.
// Low CoV → spatially uniform noise → AI suspected.
func blockLaplacianCoV(lap []float64, N, blockSize int) float64 {
	blocks := N / blockSize
	energies := make([]float64, 0, blocks*blocks)

	for by := 0; by < blocks; by++ {
		for bx := 0; bx < blocks; bx++ {
			var sum float64
			for dy := 0; dy < blockSize; dy++ {
				for dx := 0; dx < blockSize; dx++ {
					y := by*blockSize + dy
					x := bx*blockSize + dx
					sum += math.Abs(lap[y*N+x])
				}
			}
			energies = append(energies, sum/float64(blockSize*blockSize))
		}
	}

	mean, sd := meanStd(energies)
	if mean < 1e-9 {
		return 0
	}
	return sd / mean
}

// sobel2D returns gradient magnitudes via a 3×3 Sobel operator.
func sobel2D(px []float64, N int) []float64 {
	out := make([]float64, N*N)
	for y := 1; y < N-1; y++ {
		for x := 1; x < N-1; x++ {
			gx := -px[(y-1)*N+(x-1)] - 2*px[y*N+(x-1)] - px[(y+1)*N+(x-1)] +
				px[(y-1)*N+(x+1)] + 2*px[y*N+(x+1)] + px[(y+1)*N+(x+1)]
			gy := -px[(y-1)*N+(x-1)] - 2*px[(y-1)*N+x] - px[(y-1)*N+(x+1)] +
				px[(y+1)*N+(x-1)] + 2*px[(y+1)*N+x] + px[(y+1)*N+(x+1)]
			out[y*N+x] = math.Sqrt(gx*gx + gy*gy)
		}
	}
	return out
}

// flatRegionStdDev computes the standard deviation of pixel values in the
// "flat" areas of the image — pixels where the Sobel gradient is below a
// threshold (< 5% of the gradient range). Returns value in [0, 1].
func flatRegionStdDev(px, grad []float64, N int) float64 {
	// Determine gradient threshold: bottom 20th percentile of non-border pixels.
	var gradVals []float64
	for y := 1; y < N-1; y++ {
		for x := 1; x < N-1; x++ {
			gradVals = append(gradVals, grad[y*N+x])
		}
	}
	if len(gradVals) == 0 {
		return 0
	}
	// Use the 20th-percentile gradient as the "flat" threshold.
	thresh := percentile(gradVals, 20)

	var flat []float64
	for y := 1; y < N-1; y++ {
		for x := 1; x < N-1; x++ {
			if grad[y*N+x] <= thresh {
				flat = append(flat, px[y*N+x])
			}
		}
	}
	if len(flat) < 100 {
		return 0.06 // not enough flat pixels — return a neutral value
	}
	_, sd := meanStd(flat)
	return sd
}

// pearsonCorr64 computes the Pearson correlation coefficient between two
// float64 slices of equal length.
func pearsonCorr64(a, b []float64) float64 {
	n := len(a)
	if n != len(b) || n == 0 {
		return 0
	}
	var sumA, sumB float64
	for i := range a {
		sumA += a[i]
		sumB += b[i]
	}
	meanA := sumA / float64(n)
	meanB := sumB / float64(n)

	var cov, varA, varB float64
	for i := range a {
		da := a[i] - meanA
		db := b[i] - meanB
		cov += da * db
		varA += da * da
		varB += db * db
	}
	if varA < 1e-12 || varB < 1e-12 {
		return 0
	}
	return cov / math.Sqrt(varA*varB)
}

// meanStd computes the mean and standard deviation of a float64 slice.
func meanStd(xs []float64) (mean, sd float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	for _, v := range xs {
		mean += v
	}
	mean /= float64(len(xs))
	for _, v := range xs {
		d := v - mean
		sd += d * d
	}
	sd = math.Sqrt(sd / float64(len(xs)))
	return
}

// percentile returns the p-th percentile (0-100) of an unsorted slice using
// a simple counting approach (no full sort needed — we scan once).
func percentile(xs []float64, p int) float64 {
	if len(xs) == 0 {
		return 0
	}
	// Quick estimate: find the value below which p% of data falls.
	// We use a 64-bucket histogram over [0, max].
	var maxV float64
	for _, v := range xs {
		if v > maxV {
			maxV = v
		}
	}
	if maxV < 1e-12 {
		return 0
	}
	const bins = 64
	counts := make([]int, bins)
	for _, v := range xs {
		b := int(v / maxV * (bins - 1))
		counts[b]++
	}
	target := int(float64(len(xs)) * float64(p) / 100.0)
	cum := 0
	for i, c := range counts {
		cum += c
		if cum >= target {
			return float64(i) / float64(bins-1) * maxV
		}
	}
	return maxV
}

// ─── score fusion ─────────────────────────────────────────────────────────────

// statConfidence converts the three raw signals into a combined [0, 1]
// confidence score (1 = very likely AI-generated).
//
// Calibration anchors (empirically determined for diffusion/GAN face images):
//
//	noiseCoV   :  AI ≤ 0.35,  Real ≥ 0.65  →  score = clamp01((0.65 − CoV) / 0.30)
//	noiseCorr  :  AI ≥ 0.60,  Real ≤ 0.30  →  score = clamp01((corr − 0.30) / 0.40)
//	flatStd    :  AI ≤ 0.018, Real ≥ 0.045 →  score = clamp01((0.045 − std) / 0.027)
//	             (in 0-1 space; flatStd255 is the ×255 version stored in result)
func statConfidence(noiseCoV, noiseCorr, flatStd float64) (float64, []string) {
	var signals []string

	covScore := clamp01((0.65 - noiseCoV) / 0.30)
	corrScore := clamp01((noiseCorr - 0.30) / 0.40)
	flatScore := clamp01((0.045 - flatStd) / 0.027)

	if covScore > 0.5 {
		signals = append(signals, "uniform_noise_structure")
	}
	if corrScore > 0.5 {
		signals = append(signals, "high_interchannel_noise_correlation")
	}
	if flatScore > 0.5 {
		signals = append(signals, "unnaturally_smooth_flat_regions")
	}

	// Weighted combination — noise uniformity is the strongest signal.
	combined := 0.40*covScore + 0.35*corrScore + 0.25*flatScore

	return math.Round(combined*1000) / 1000, signals
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
