// Package model define os DTOs usados por API, workers e persistência.
package model

import "time"

// MetadataResult representa o resultado da análise de metadados EXIF/XMP/IPTC.
type MetadataResult struct {
	Suspicious       bool      `json:"suspicious"`
	Software         string    `json:"software,omitempty"`
	Camera           string    `json:"camera,omitempty"`
	DateTimeOriginal time.Time `json:"datetime_original,omitempty"`
	DateTimeModified time.Time `json:"datetime_modified,omitempty"`
	GPSPresent       bool      `json:"gps_present"`
	Confidence       float64   `json:"confidence"`
	Reasons          []string  `json:"reasons,omitempty"`
}

// ELAResult representa o resultado da Error Level Analysis.
type ELAResult struct {
	HasEdits       bool    `json:"has_edits"`
	EditPercentage float64 `json:"edit_percentage"`
	Confidence     float64 `json:"confidence"`
	HeatmapURL     string  `json:"heatmap_url,omitempty"`
	HeatmapPNG     []byte  `json:"-"`
}

// AIResult representa o veredito do modelo ONNX de detecção de IA generativa.
type AIResult struct {
	IsAIGenerated bool    `json:"is_ai_generated"`
	Confidence    float64 `json:"confidence"`
	ModelVersion  string  `json:"model_version"`
}

// FrequencyResult representa a análise espectral FFT.
type FrequencyResult struct {
	IsSuspicious  bool    `json:"is_suspicious"`
	Confidence    float64 `json:"confidence"`
	HighFreqRatio float64 `json:"high_freq_ratio"`
	LowFreqRatio  float64 `json:"low_freq_ratio"`
}

// StatisticalResult representa a análise estatística de pixels para detecção de
// imagens geradas por IA, sem necessidade de modelo ONNX.
type StatisticalResult struct {
	// NoiseCoV é o coeficiente de variação do ruído por bloco.
	// Imagens de câmera real: CoV alto (ruído irregular). IA: CoV baixo (ruído uniforme).
	NoiseCoV float64 `json:"noise_cov"`
	// NoiseCorrelation é a correlação de Pearson entre o ruído dos canais R e B.
	// IA sintetiza os canais em conjunto → correlação alta. Câmera real: baixa.
	NoiseCorrelation float64 `json:"noise_correlation"`
	// FlatRegionSmooth é o desvio padrão de pixel em regiões planas (escala 0-255).
	// IA: pele/fundo sinteticamente lisos → valor baixo. Real: grão de câmera → valor alto.
	FlatRegionSmooth float64 `json:"flat_region_smooth"`
	// Confidence é a pontuação final combinada (0 = autêntico, 1 = IA detectada).
	Confidence    float64  `json:"confidence"`
	IsAISuspected bool     `json:"is_ai_suspected"`
	Signals       []string `json:"signals,omitempty"`
}

// VerificationResult é o resultado consolidado devolvido ao cliente.
type VerificationResult struct {
	ID               string                 `json:"id"`
	Status           string                 `json:"status,omitempty"`
	Authentic        bool                   `json:"authentic"`
	Confidence       float64                `json:"confidence"`
	Recommendation   string                 `json:"recommendation"`
	Priority         string                 `json:"priority"`
	Summary          string                 `json:"summary"`
	Analysis         AnalysisBundle         `json:"analysis"`
	Scores           map[string]float64     `json:"scores"`
	ModelVersions    map[string]string      `json:"model_versions"`
	ProcessingTimeMs int                    `json:"processing_time_ms"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	Timestamp        time.Time              `json:"timestamp"`
}

// AnalysisBundle empacota os sub-resultados de todos os módulos.
type AnalysisBundle struct {
	Metadata    *MetadataResult    `json:"metadata,omitempty"`
	ELA         *ELAResult         `json:"ela,omitempty"`
	AI          *AIResult          `json:"ai,omitempty"`
	Frequency   *FrequencyResult   `json:"frequency,omitempty"`
	Statistical *StatisticalResult `json:"statistical,omitempty"`
}

// VerificationRequestedEvent é publicado em `verify.requested`.
type VerificationRequestedEvent struct {
	VerificationID string    `json:"verification_id"`
	TenantID       string    `json:"tenant_id"`
	S3Key          string    `json:"s3_key"`
	SHA256         string    `json:"sha256"`
	OrderID        string    `json:"order_id,omitempty"`
	Plan           string    `json:"plan,omitempty"`
	Profile        string    `json:"profile,omitempty"`
	RequestedAt    time.Time `json:"requested_at"`
}

// WebhookPayload é a estrutura entregue a `webhook.dispatch`.
type WebhookPayload struct {
	VerificationID string              `json:"verification_id"`
	TenantID       string              `json:"tenant_id"`
	Result         *VerificationResult `json:"result"`
}

// Recomendações
const (
	RecommendationAccept = "ACCEPT"
	RecommendationReview = "REVIEW"
	RecommendationReject = "REJECT"

	PriorityLow    = "LOW"
	PriorityMedium = "MEDIUM"
	PriorityHigh   = "HIGH"
)
