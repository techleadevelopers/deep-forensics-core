package evaluation

import (
	"math"
	"sort"
)

// CalibrationBin representa um bucket de scores com a fração real de fraudulentos.
type CalibrationBin struct {
	ScoreLow            float64 `json:"score_low"`
	ScoreHigh           float64 `json:"score_high"`
	Count               int     `json:"count"`
	MeanScore           float64 `json:"mean_score"`
	FractionManipulated float64 `json:"fraction_manipulated"`
	// CalibrationError = |FractionManipulated - MeanScore/100|
	// Um sistema bem calibrado teria CalibrationError ≈ 0.
	CalibrationError float64 `json:"calibration_error"`
}

// CalibrationReport é o resultado da análise de calibração.
type CalibrationReport struct {
	Bins             []CalibrationBin `json:"bins"`
	ECE              float64          `json:"ece"`  // Expected Calibration Error
	MCE              float64          `json:"mce"`  // Maximum Calibration Error
	ThresholdSweep   []ThresholdPoint `json:"threshold_sweep"`
	OptimalThreshold float64          `json:"optimal_threshold"`
	OptimalF1        float64          `json:"optimal_f1"`
}

// ThresholdPoint registra métricas de desempenho para um threshold específico.
type ThresholdPoint struct {
	Threshold float64 `json:"threshold"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
	FPR       float64 `json:"fpr"`
	FNR       float64 `json:"fnr"`
	Accuracy  float64 `json:"accuracy"`
}

// ROCPoint é um ponto da curva ROC (usado por metrics.go e report).
type ROCPoint struct {
	Threshold float64 `json:"threshold"`
	FPR       float64 `json:"fpr"`
	TPR       float64 `json:"tpr"`
}

// calSample é o tipo interno usado nas funções de calibração.
type calSample struct {
	score       float64
	manipulated bool
}

// ComputeCalibration gera o relatório de calibração com nBins buckets.
func ComputeCalibration(results []PredictionResult, nBins int) *CalibrationReport {
	if nBins <= 0 {
		nBins = 10
	}

	var samples []calSample
	for i := range results {
		r := &results[i]
		if r.Error != "" && r.Result == nil {
			continue
		}
		samples = append(samples, calSample{
			score:       r.Score,
			manipulated: isManipulatedLabel(r.Entry.Label),
		})
	}

	if len(samples) == 0 {
		return &CalibrationReport{}
	}

	binWidth := 100.0 / float64(nBins)
	bins := make([]CalibrationBin, nBins)
	for i := range bins {
		bins[i].ScoreLow = float64(i) * binWidth
		bins[i].ScoreHigh = bins[i].ScoreLow + binWidth
	}

	for _, s := range samples {
		idx := int(s.score / binWidth)
		if idx >= nBins {
			idx = nBins - 1
		}
		bins[idx].Count++
		bins[idx].MeanScore += s.score
		if s.manipulated {
			bins[idx].FractionManipulated++
		}
	}

	total := float64(len(samples))
	var ece, mce float64
	for i := range bins {
		b := &bins[i]
		if b.Count == 0 {
			continue
		}
		b.MeanScore /= float64(b.Count)
		b.FractionManipulated /= float64(b.Count)
		b.CalibrationError = math.Abs(b.FractionManipulated - b.MeanScore/100.0)
		ece += (float64(b.Count) / total) * b.CalibrationError
		if b.CalibrationError > mce {
			mce = b.CalibrationError
		}
		b.MeanScore = roundTo(b.MeanScore, 2)
		b.FractionManipulated = roundTo(b.FractionManipulated, 4)
		b.CalibrationError = roundTo(b.CalibrationError, 4)
	}

	sweep := computeThresholdSweep(samples)
	optThreshold, optF1 := findOptimalThreshold(sweep)

	return &CalibrationReport{
		Bins:             bins,
		ECE:              roundTo(ece, 4),
		MCE:              roundTo(mce, 4),
		ThresholdSweep:   sweep,
		OptimalThreshold: optThreshold,
		OptimalF1:        roundTo(optF1, 4),
	}
}

func computeThresholdSweep(samples []calSample) []ThresholdPoint {
	points := make([]ThresholdPoint, 0, 19)
	for t := 5.0; t <= 95.0; t += 5.0 {
		var tp, tn, fp, fn int
		for _, s := range samples {
			pred := s.score >= t
			switch {
			case pred && s.manipulated:
				tp++
			case !pred && !s.manipulated:
				tn++
			case pred && !s.manipulated:
				fp++
			case !pred && s.manipulated:
				fn++
			}
		}
		ttl := tp + tn + fp + fn
		prec, rec, f1, fpr, fnr, acc := 0.0, 0.0, 0.0, 0.0, 0.0, 0.0
		if tp+fp > 0 {
			prec = float64(tp) / float64(tp+fp)
		}
		if tp+fn > 0 {
			rec = float64(tp) / float64(tp+fn)
		}
		if prec+rec > 0 {
			f1 = 2 * prec * rec / (prec + rec)
		}
		if fp+tn > 0 {
			fpr = float64(fp) / float64(fp+tn)
		}
		if fn+tp > 0 {
			fnr = float64(fn) / float64(fn+tp)
		}
		if ttl > 0 {
			acc = float64(tp+tn) / float64(ttl)
		}
		points = append(points, ThresholdPoint{
			Threshold: t,
			Precision: roundTo(prec, 4),
			Recall:    roundTo(rec, 4),
			F1:        roundTo(f1, 4),
			FPR:       roundTo(fpr, 4),
			FNR:       roundTo(fnr, 4),
			Accuracy:  roundTo(acc, 4),
		})
	}
	return points
}

func findOptimalThreshold(sweep []ThresholdPoint) (threshold, f1 float64) {
	if len(sweep) == 0 {
		return 50.0, 0
	}
	best := sweep[0]
	for _, p := range sweep[1:] {
		if p.F1 > best.F1 {
			best = p
		}
	}
	return best.Threshold, best.F1
}

// PlattScaling ajusta os scores usando calibração logística.
// Retorna os coeficientes (A, B) tal que:
//
//	P(manipulated | score) = sigmoid(A * (score/100) + B)
//
// Para usar: calibrated_prob = 1 / (1 + exp(-(A*(score/100) + B)))
func PlattScaling(results []PredictionResult) (A, B float64) {
	var pts []calSample
	for i := range results {
		r := &results[i]
		if r.Error != "" && r.Result == nil {
			continue
		}
		lbl := 0.0
		if isManipulatedLabel(r.Entry.Label) {
			lbl = 1.0
		}
		pts = append(pts, calSample{score: r.Score / 100.0, manipulated: lbl == 1.0})
	}
	if len(pts) < 2 {
		return 1.0, -0.5
	}

	A, B = 1.0, -0.5
	lr := 0.01
	for iter := 0; iter < 2000; iter++ {
		var dA, dB float64
		for _, p := range pts {
			z := A*p.score + B
			sig := sigmoid(z)
			lbl := 0.0
			if p.manipulated {
				lbl = 1.0
			}
			err := sig - lbl
			dA += err * p.score
			dB += err
		}
		n := float64(len(pts))
		A -= lr * dA / n
		B -= lr * dB / n
	}

	// Garante que score alto = maior P(manipulado)
	if A < 0 {
		A = -A
		B = -B
	}

	return roundTo(A, 4), roundTo(B, 4)
}

func sigmoid(z float64) float64 {
	return 1.0 / (1.0 + math.Exp(-z))
}

// SortedThresholds retorna os thresholds onde o score ótimo muda (útil para PR curve).
func SortedThresholds(results []PredictionResult) []float64 {
	seen := make(map[float64]bool)
	for i := range results {
		if results[i].Error == "" || results[i].Result != nil {
			seen[results[i].Score] = true
		}
	}
	thresholds := make([]float64, 0, len(seen))
	for t := range seen {
		thresholds = append(thresholds, t)
	}
	sort.Float64s(thresholds)
	return thresholds
}
