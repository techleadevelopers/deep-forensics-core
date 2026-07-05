package analyzer

import (
	"fmt"
	"time"

	"github.com/PixelAudit/PixelAudit/internal/model"
)

// Weights define os pesos de cada módulo de análise.
// Não precisam somar 1.0 — Combine normaliza pelo peso ativo total.
type Weights struct {
	Metadata    float64
	ELA         float64
	AI          float64
	Frequency   float64
	Statistical float64
}

// DefaultWeights são os pesos oficiais do produto.
// O módulo Statistical sempre roda (independe de ONNX/ProfileFull),
// por isso tem peso expressivo mesmo sem o detector de IA.
var DefaultWeights = Weights{
	Metadata:    0.20,
	ELA:         0.15,
	AI:          0.35,
	Frequency:   0.15,
	Statistical: 0.25,
}

// Fusion combina os sub-resultados em um veredito final.
type Fusion struct{ w Weights }

// NewFusion cria a fusão com pesos customizados.
func NewFusion(w Weights) *Fusion { return &Fusion{w: w} }

// Combine gera o VerificationResult final.
// Módulos ausentes (nil) são excluídos e os pesos são renormalizados —
// assim um detector de IA indisponível não "dilui" os demais sinais.
func (f *Fusion) Combine(
	m *model.MetadataResult,
	e *model.ELAResult,
	a *model.AIResult,
	fr *model.FrequencyResult,
	st *model.StatisticalResult,
) *model.VerificationResult {
	scores := map[string]float64{}
	raw := 0.0
	activeWeight := 0.0

	if m != nil {
		w := f.w.Metadata
		s := m.Confidence * w
		scores["metadata"] = s
		raw += s
		activeWeight += w
	}
	if e != nil {
		w := f.w.ELA
		s := e.Confidence * w
		scores["ela"] = s
		raw += s
		activeWeight += w
	}
	if a != nil {
		w := f.w.AI
		s := a.Confidence * w
		scores["ai"] = s
		raw += s
		activeWeight += w
	}
	if fr != nil {
		w := f.w.Frequency
		s := fr.Confidence * w
		scores["frequency"] = s
		raw += s
		activeWeight += w
	}
	if st != nil {
		w := f.w.Statistical
		s := st.Confidence * w
		scores["statistical"] = s
		raw += s
		activeWeight += w
	}

	// Normalize so the threshold of 0.5 is always valid regardless of which
	// modules are present.
	total := 0.0
	if activeWeight > 0 {
		total = raw / activeWeight
	}

	authentic := total < 0.5
	rec, prio := recommendation(total)

	versions := map[string]string{}
	if a != nil {
		versions["ai"] = a.ModelVersion
	}
	versions["statistical"] = "stat_v1.0"

	return &model.VerificationResult{
		Authentic:      authentic,
		Confidence:     roundTo(total*100, 2),
		Recommendation: rec,
		Priority:       prio,
		Summary:        summarize(authentic, total, a, e, st),
		Analysis: model.AnalysisBundle{
			Metadata:    m,
			ELA:         e,
			AI:          a,
			Frequency:   fr,
			Statistical: st,
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

func summarize(authentic bool, score float64, ai *model.AIResult, ela *model.ELAResult, st *model.StatisticalResult) string {
	pct := score * 100
	if authentic {
		return fmt.Sprintf("Imagem parece autêntica. Score de manipulação: %.1f%%.", pct)
	}

	aiPct, elaPct, statPct := 0.0, 0.0, 0.0
	if ai != nil {
		aiPct = ai.Confidence * 100
	}
	if ela != nil {
		elaPct = ela.Confidence * 100
	}
	if st != nil {
		statPct = st.Confidence * 100
	}

	if aiPct > 0 {
		return fmt.Sprintf(
			"Imagem suspeita de manipulação/geração por IA (score %.1f%%). Modelo ONNX: %.0f%%. Análise estatística: %.0f%%. Evidências de edição: %.0f%%.",
			pct, aiPct, statPct, elaPct,
		)
	}
	return fmt.Sprintf(
		"Imagem suspeita de geração por IA (score %.1f%%). Análise estatística: %.0f%%. Evidências de edição: %.0f%%.",
		pct, statPct, elaPct,
	)
}

func roundTo(v float64, digits int) float64 {
	p := 1.0
	for i := 0; i < digits; i++ {
		p *= 10
	}
	return float64(int(v*p+0.5)) / p
}
