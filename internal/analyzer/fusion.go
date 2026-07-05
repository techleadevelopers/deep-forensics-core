package analyzer

import (
	"fmt"
	"time"

	"github.com/PixelAudit/PixelAudit/internal/model"
)

// Weights define os pesos aplicados a cada camada de análise.
type Weights struct {
	Metadata  float64
	ELA       float64
	AI        float64
	Frequency float64
}

// DefaultWeights corresponde ao esquema oficial do produto.
var DefaultWeights = Weights{
	Metadata:  0.25,
	ELA:       0.25,
	AI:        0.35,
	Frequency: 0.15,
}

// Fusion combina os 4 sub-resultados em um veredito final.
type Fusion struct{ w Weights }

// NewFusion cria a fusão com pesos customizados.
func NewFusion(w Weights) *Fusion { return &Fusion{w: w} }

// Combine gera o VerificationResult final.
func (f *Fusion) Combine(m *model.MetadataResult, e *model.ELAResult, a *model.AIResult, fr *model.FrequencyResult) *model.VerificationResult {
	scores := map[string]float64{}
	total := 0.0

	if m != nil {
		s := m.Confidence * f.w.Metadata
		scores["metadata"] = s
		total += s
	}
	if e != nil {
		s := e.Confidence * f.w.ELA
		scores["ela"] = s
		total += s
	}
	if a != nil {
		s := a.Confidence * f.w.AI
		scores["ai"] = s
		total += s
	}
	if fr != nil {
		s := fr.Confidence * f.w.Frequency
		scores["frequency"] = s
		total += s
	}

	authentic := total < 0.5
	rec, prio := recommendation(total)

	versions := map[string]string{}
	if a != nil {
		versions["ai"] = a.ModelVersion
	}

	return &model.VerificationResult{
		Authentic:      authentic,
		Confidence:     roundTo(total*100, 2),
		Recommendation: rec,
		Priority:       prio,
		Summary:        summarize(authentic, total, a, e),
		Analysis: model.AnalysisBundle{
			Metadata:  m,
			ELA:       e,
			AI:        a,
			Frequency: fr,
		},
		Scores:        scores,
		ModelVersions: versions,
		Timestamp:     time.Now().UTC(),
	}
}

func recommendation(score float64) (string, string) {
	switch {
	case score >= 0.75:
		return model.RecommendationReject, model.PriorityHigh
	case score >= 0.45:
		return model.RecommendationReview, model.PriorityMedium
	default:
		return model.RecommendationAccept, model.PriorityLow
	}
}

func summarize(authentic bool, score float64, ai *model.AIResult, ela *model.ELAResult) string {
	if authentic {
		return fmt.Sprintf("Imagem parece autêntica. Score de manipulação: %.1f%%.", score*100)
	}
	aiPct, elaPct := 0.0, 0.0
	if ai != nil {
		aiPct = ai.Confidence * 100
	}
	if ela != nil {
		elaPct = ela.Confidence * 100
	}
	return fmt.Sprintf(
		"Imagem suspeita de manipulação (score %.1f%%). Detecção de IA: %.0f%%. Evidências de edição: %.0f%%.",
		score*100, aiPct, elaPct,
	)
}

func roundTo(v float64, digits int) float64 {
	p := 1.0
	for i := 0; i < digits; i++ {
		p *= 10
	}
	return float64(int(v*p+0.5)) / p
}
