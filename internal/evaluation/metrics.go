// Package evaluation implementa o harness de métricas do PixelAudit.
package evaluation

import (
	"math"
	"sort"
)

// ClassMetrics agrupa as métricas binárias de detecção de fraude.
//
// Convenção adotada:
//
//	POSITIVO  = imagem manipulada/fraudulenta (alvo da detecção)
//	NEGATIVO  = imagem autêntica
type ClassMetrics struct {
	TP int `json:"tp"` // Verdadeiro Positivo: detectou fraude que existe
	TN int `json:"tn"` // Verdadeiro Negativo: liberou imagem legítima
	FP int `json:"fp"` // Falso Positivo: falso alarme (imagem legítima marcada como fraude)
	FN int `json:"fn"` // Falso Negativo: fraude não detectada

	Total    int `json:"total"`
	Positive int `json:"positive"` // total de imagens manipuladas no dataset
	Negative int `json:"negative"` // total de imagens autênticas no dataset

	Accuracy    float64 `json:"accuracy"`
	Precision   float64 `json:"precision"`
	Recall      float64 `json:"recall"`      // = Sensitivity = TPR
	F1          float64 `json:"f1_score"`
	Specificity float64 `json:"specificity"` // = TNR
	FPR         float64 `json:"false_positive_rate"` // = 1 - Specificity
	FNR         float64 `json:"false_negative_rate"` // = 1 - Recall
	MCC         float64 `json:"mcc"`   // Matthews Correlation Coefficient
	AUROC       float64 `json:"auroc"` // Area Under ROC Curve
}

// LatencyStats armazena percentis de latência em millisegundos.
type LatencyStats struct {
	Min  float64 `json:"min_ms"`
	Max  float64 `json:"max_ms"`
	Mean float64 `json:"mean_ms"`
	P50  float64 `json:"p50_ms"`
	P90  float64 `json:"p90_ms"`
	P95  float64 `json:"p95_ms"`
	P99  float64 `json:"p99_ms"`
}

// ScoreStats agrega estatísticas do score de manipulação por classe.
type ScoreStats struct {
	Label  string  `json:"label"`
	Count  int     `json:"count"`
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"std_dev"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	P25    float64 `json:"p25"`
	P50    float64 `json:"p50"`
	P75    float64 `json:"p75"`
}

// Metrics é o resultado completo da avaliação.
type Metrics struct {
	Binary          ClassMetrics              `json:"binary_metrics"`
	Latency         LatencyStats              `json:"latency"`
	ScoreByLabel    map[string]*ScoreStats    `json:"score_by_label"`
	ScoreByCategory map[string]*ScoreStats    `json:"score_by_category"`
	Threshold       float64                   `json:"threshold"`
	PipelineErrors  int                       `json:"pipeline_errors"`
}

// rocPair é um par (score, isManipulated) usado para computar AUROC.
type rocPair struct {
	score       float64
	manipulated bool
}

// ComputeMetrics calcula todas as métricas a partir dos resultados de predição.
// threshold é o score (0–100) acima do qual a imagem é classificada como manipulada.
func ComputeMetrics(results []PredictionResult, threshold float64) Metrics {
	m := Metrics{
		Threshold:       threshold,
		ScoreByLabel:    make(map[string]*ScoreStats),
		ScoreByCategory: make(map[string]*ScoreStats),
	}

	var latencies []float64
	scoresByLabel := make(map[string][]float64)
	scoresByCategory := make(map[string][]float64)
	var roc []rocPair

	for i := range results {
		r := &results[i]

		if r.Error != "" && r.Result == nil {
			m.PipelineErrors++
			continue
		}

		actualManip := isManipulatedLabel(r.Entry.Label)
		predictedManip := r.Score >= threshold

		switch {
		case predictedManip && actualManip:
			m.Binary.TP++
		case !predictedManip && !actualManip:
			m.Binary.TN++
		case predictedManip && !actualManip:
			m.Binary.FP++
		case !predictedManip && actualManip:
			m.Binary.FN++
		}

		m.Binary.Total++
		if actualManip {
			m.Binary.Positive++
		} else {
			m.Binary.Negative++
		}

		latencies = append(latencies, float64(r.LatencyMs))
		scoresByLabel[r.Entry.Label] = append(scoresByLabel[r.Entry.Label], r.Score)
		if r.Entry.Category != "" {
			scoresByCategory[r.Entry.Category] = append(scoresByCategory[r.Entry.Category], r.Score)
		}
		roc = append(roc, rocPair{score: r.Score, manipulated: actualManip})
	}

	cm := &m.Binary
	if cm.Total > 0 {
		cm.Accuracy = float64(cm.TP+cm.TN) / float64(cm.Total)
	}
	if cm.TP+cm.FP > 0 {
		cm.Precision = float64(cm.TP) / float64(cm.TP+cm.FP)
	}
	if cm.TP+cm.FN > 0 {
		cm.Recall = float64(cm.TP) / float64(cm.TP+cm.FN)
	}
	if cm.Precision+cm.Recall > 0 {
		cm.F1 = 2 * cm.Precision * cm.Recall / (cm.Precision + cm.Recall)
	}
	if cm.TN+cm.FP > 0 {
		cm.Specificity = float64(cm.TN) / float64(cm.TN+cm.FP)
		cm.FPR = 1 - cm.Specificity
	}
	if cm.TP+cm.FN > 0 {
		cm.FNR = float64(cm.FN) / float64(cm.TP+cm.FN)
	}
	cm.MCC = computeMCC(cm.TP, cm.TN, cm.FP, cm.FN)
	cm.AUROC = computeAUROC(roc)

	// Round derived metrics
	cm.Accuracy = roundTo(cm.Accuracy, 4)
	cm.Precision = roundTo(cm.Precision, 4)
	cm.Recall = roundTo(cm.Recall, 4)
	cm.F1 = roundTo(cm.F1, 4)
	cm.Specificity = roundTo(cm.Specificity, 4)
	cm.FPR = roundTo(cm.FPR, 4)
	cm.FNR = roundTo(cm.FNR, 4)
	cm.MCC = roundTo(cm.MCC, 4)
	cm.AUROC = roundTo(cm.AUROC, 4)

	m.Latency = computeLatencyStats(latencies)

	for label, scores := range scoresByLabel {
		m.ScoreByLabel[label] = computeScoreStats(label, scores)
	}
	for cat, scores := range scoresByCategory {
		m.ScoreByCategory[cat] = computeScoreStats(cat, scores)
	}

	return m
}

func computeMCC(tp, tn, fp, fn int) float64 {
	num := float64(tp*tn - fp*fn)
	denom := math.Sqrt(float64((tp + fp) * (tp + fn) * (tn + fp) * (tn + fn)))
	if denom == 0 {
		return 0
	}
	return num / denom
}

func computeAUROC(pairs []rocPair) float64 {
	if len(pairs) == 0 {
		return 0.5
	}
	sorted := make([]rocPair, len(pairs))
	copy(sorted, pairs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	var totalPos, totalNeg int
	for _, p := range sorted {
		if p.manipulated {
			totalPos++
		} else {
			totalNeg++
		}
	}
	if totalPos == 0 || totalNeg == 0 {
		return 0.5
	}

	var prevTPR, prevFPR float64
	var area float64
	var tp, fp int

	for i, p := range sorted {
		if p.manipulated {
			tp++
		} else {
			fp++
		}
		tpr := float64(tp) / float64(totalPos)
		fpr := float64(fp) / float64(totalNeg)
		if i > 0 {
			area += (fpr - prevFPR) * (tpr + prevTPR) / 2
		}
		prevTPR = tpr
		prevFPR = fpr
	}
	return area
}

// ComputeROC calcula pontos da curva ROC para todos os thresholds únicos.
func ComputeROC(results []PredictionResult) []ROCPoint {
	var pairs []rocPair
	var totalPos, totalNeg int
	for i := range results {
		r := &results[i]
		if r.Error != "" && r.Result == nil {
			continue
		}
		m := isManipulatedLabel(r.Entry.Label)
		pairs = append(pairs, rocPair{r.Score, m})
		if m {
			totalPos++
		} else {
			totalNeg++
		}
	}
	if totalPos == 0 || totalNeg == 0 {
		return nil
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	var points []ROCPoint
	var tp, fp int
	points = append(points, ROCPoint{Threshold: 100.0, FPR: 0, TPR: 0})

	for _, p := range pairs {
		if p.manipulated {
			tp++
		} else {
			fp++
		}
		points = append(points, ROCPoint{
			Threshold: p.score,
			FPR:       roundTo(float64(fp)/float64(totalNeg), 4),
			TPR:       roundTo(float64(tp)/float64(totalPos), 4),
		})
	}
	return points
}

func computeLatencyStats(latencies []float64) LatencyStats {
	if len(latencies) == 0 {
		return LatencyStats{}
	}
	sorted := make([]float64, len(latencies))
	copy(sorted, latencies)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	return LatencyStats{
		Min:  sorted[0],
		Max:  sorted[len(sorted)-1],
		Mean: roundTo(mean, 2),
		P50:  percentile(sorted, 50),
		P90:  percentile(sorted, 90),
		P95:  percentile(sorted, 95),
		P99:  percentile(sorted, 99),
	}
}

func computeScoreStats(label string, scores []float64) *ScoreStats {
	if len(scores) == 0 {
		return &ScoreStats{Label: label}
	}
	sorted := make([]float64, len(scores))
	copy(sorted, scores)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(len(sorted))

	var variance float64
	for _, v := range sorted {
		d := v - mean
		variance += d * d
	}
	if len(sorted) > 1 {
		variance /= float64(len(sorted) - 1)
	}

	return &ScoreStats{
		Label:  label,
		Count:  len(sorted),
		Mean:   roundTo(mean, 4),
		StdDev: roundTo(math.Sqrt(variance), 4),
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
		P25:    percentile(sorted, 25),
		P50:    percentile(sorted, 50),
		P75:    percentile(sorted, 75),
	}
}

// percentile calcula o percentil p (0–100) de uma slice já ordenada.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p / 100) * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (idx-float64(lo))*(sorted[hi]-sorted[lo])
}

func roundTo(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

func isManipulatedLabel(label string) bool {
	switch label {
	case "manipulated", "ai_generated", "partial":
		return true
	}
	return false
}
