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

// VerificationResult é o resultado consolidado devolvido ao cliente.
type VerificationResult struct {
	ID               string                 `json:"id"`
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

// AnalysisBundle empacota os 4 sub-resultados.
type AnalysisBundle struct {
	Metadata  *MetadataResult  `json:"metadata,omitempty"`
	ELA       *ELAResult       `json:"ela,omitempty"`
	AI        *AIResult        `json:"ai,omitempty"`
	Frequency *FrequencyResult `json:"frequency,omitempty"`
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
