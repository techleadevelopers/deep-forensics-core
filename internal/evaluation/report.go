package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EvalSummary é o relatório principal gerado em reports/eval-summary.json.
type EvalSummary struct {
	// Versão do schema do relatório para compatibilidade futura.
	SchemaVersion string `json:"schema_version"`
	// Timestamp da execução.
	GeneratedAt time.Time `json:"generated_at"`
	// Metadados da execução.
	Run RunMeta `json:"run"`
	// Métricas completas.
	Metrics Metrics `json:"metrics"`
	// Recomendação de threshold baseada na calibração.
	RecommendedThreshold float64 `json:"recommended_threshold"`
	// Informações sobre o modelo ONNX (se disponível).
	ModelInfo map[string]string `json:"model_info,omitempty"`
}

// RunMeta contém metadados sobre a execução do eval.
type RunMeta struct {
	ManifestPath string         `json:"manifest_path"`
	TotalSamples int            `json:"total_samples"`
	LoadedImages int            `json:"loaded_images"`
	Skipped      int            `json:"skipped_images"`
	Threshold    float64        `json:"decision_threshold"`
	Workers      int            `json:"workers"`
	AIEnabled    bool           `json:"ai_detector_enabled"`
	DatasetStats map[string]int `json:"dataset_stats"` // contagem por label
	DurationMs   int64          `json:"duration_ms"`
}

// FalseCase representa um FP ou FN exportado para JSONL.
type FalseCase struct {
	Path      string  `json:"path"`
	Label     string  `json:"label"`
	Category  string  `json:"category"`
	Score     float64 `json:"score"`
	LatencyMs int64   `json:"latency_ms"`
	Notes     string  `json:"notes,omitempty"`
	// SubScores detalha os scores de cada analisador para diagnóstico.
	SubScores map[string]float64 `json:"sub_scores,omitempty"`
}

// ReportPaths agrupa os caminhos dos arquivos gerados.
type ReportPaths struct {
	Summary         string
	ConfusionMatrix string
	Calibration     string
	ROCCurve        string
	FalsePositives  string
	FalseNegatives  string
}

// DefaultReportPaths retorna os caminhos padrão sob reportsDir.
func DefaultReportPaths(reportsDir string) ReportPaths {
	return ReportPaths{
		Summary:         filepath.Join(reportsDir, "eval-summary.json"),
		ConfusionMatrix: filepath.Join(reportsDir, "confusion-matrix.json"),
		Calibration:     filepath.Join(reportsDir, "calibration.json"),
		ROCCurve:        filepath.Join(reportsDir, "roc-curve.json"),
		FalsePositives:  filepath.Join(reportsDir, "false-positives.jsonl"),
		FalseNegatives:  filepath.Join(reportsDir, "false-negatives.jsonl"),
	}
}

// WriteReports gera todos os relatórios JSON/JSONL no diretório especificado.
func WriteReports(
	reportsDir string,
	summary *EvalSummary,
	cm *ConfusionMatrix,
	cal *CalibrationReport,
	roc []ROCPoint,
	results []PredictionResult,
	threshold float64,
) (ReportPaths, error) {
	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		return ReportPaths{}, fmt.Errorf("criando diretório de relatórios %q: %w", reportsDir, err)
	}

	paths := DefaultReportPaths(reportsDir)

	// 1. eval-summary.json
	if err := writeJSON(paths.Summary, summary); err != nil {
		return paths, fmt.Errorf("escrevendo eval-summary: %w", err)
	}

	// 2. confusion-matrix.json
	if err := writeJSON(paths.ConfusionMatrix, cm); err != nil {
		return paths, fmt.Errorf("escrevendo confusion-matrix: %w", err)
	}

	// 3. calibration.json
	if err := writeJSON(paths.Calibration, cal); err != nil {
		return paths, fmt.Errorf("escrevendo calibration: %w", err)
	}

	// 4. roc-curve.json
	if err := writeJSON(paths.ROCCurve, roc); err != nil {
		return paths, fmt.Errorf("escrevendo roc-curve: %w", err)
	}

	// 5. false-positives.jsonl
	fps := FilterFalsePositives(results, threshold)
	if err := writeFalseCasesJSONL(paths.FalsePositives, fps); err != nil {
		return paths, fmt.Errorf("escrevendo false-positives: %w", err)
	}

	// 6. false-negatives.jsonl
	fns := FilterFalseNegatives(results, threshold)
	if err := writeFalseCasesJSONL(paths.FalseNegatives, fns); err != nil {
		return paths, fmt.Errorf("escrevendo false-negatives: %w", err)
	}

	return paths, nil
}

func writeJSON(path string, v interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("criando %q: %w", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeFalseCasesJSONL(path string, results []PredictionResult) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("criando %q: %w", path, err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range results {
		fc := toFalseCase(r)
		if err := enc.Encode(fc); err != nil {
			return fmt.Errorf("escrevendo entrada %q: %w", r.Entry.Path, err)
		}
	}
	return nil
}

func toFalseCase(r PredictionResult) FalseCase {
	fc := FalseCase{
		Path:      r.Entry.Path,
		Label:     r.Entry.Label,
		Category:  r.Entry.Category,
		Score:     r.Score,
		LatencyMs: r.LatencyMs,
		Notes:     r.Entry.Notes,
	}
	if r.Result != nil {
		fc.SubScores = r.Result.Scores
	}
	return fc
}

// PrintSummary imprime um resumo legível no stdout.
func PrintSummary(summary *EvalSummary, cm *ConfusionMatrix, cal *CalibrationReport, paths ReportPaths) {
	m := summary.Metrics
	b := m.Binary

	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Println("              VERIFOOD — EVAL SUMMARY")
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Printf("Dataset:       %s\n", summary.Run.ManifestPath)
	fmt.Printf("Imagens:       %d carregadas / %d total\n", summary.Run.LoadedImages, summary.Run.TotalSamples)
	fmt.Printf("Threshold:     %.1f\n", summary.Run.Threshold)
	fmt.Printf("AI Detector:   %v\n", summary.Run.AIEnabled)
	fmt.Printf("Duração:       %dms\n", summary.Run.DurationMs)
	fmt.Println("───────────────────────────────────────────────────────")
	fmt.Printf("Accuracy:      %.2f%%\n", b.Accuracy*100)
	fmt.Printf("Precision:     %.2f%%\n", b.Precision*100)
	fmt.Printf("Recall:        %.2f%%\n", b.Recall*100)
	fmt.Printf("F1 Score:      %.4f\n", b.F1)
	fmt.Printf("AUROC:         %.4f\n", b.AUROC)
	fmt.Printf("MCC:           %.4f\n", b.MCC)
	fmt.Println("───────────────────────────────────────────────────────")
	fmt.Printf("FPR:           %.2f%% (falsos alarmes em imagens autênticas)\n", b.FPR*100)
	fmt.Printf("FNR:           %.2f%% (fraudes não detectadas)\n", b.FNR*100)
	fmt.Printf("FP count:      %d\n", b.FP)
	fmt.Printf("FN count:      %d\n", b.FN)
	fmt.Println("───────────────────────────────────────────────────────")
	fmt.Printf("Latência p50:  %.1fms\n", m.Latency.P50)
	fmt.Printf("Latência p95:  %.1fms\n", m.Latency.P95)
	fmt.Printf("Latência p99:  %.1fms\n", m.Latency.P99)
	fmt.Println("───────────────────────────────────────────────────────")
	fmt.Printf("ECE:           %.4f (calibration error)\n", 0.0)
	if cal != nil {
		fmt.Printf("ECE:           %.4f\n", cal.ECE)
		fmt.Printf("Threshold ótimo (F1): %.1f → F1=%.4f\n", cal.OptimalThreshold, cal.OptimalF1)
	}
	fmt.Println("───────────────────────────────────────────────────────")
	fmt.Printf("\nScore médio por classe:\n")
	for label, stats := range m.ScoreByLabel {
		fmt.Printf("  %-20s mean=%.1f  std=%.1f  p50=%.1f\n",
			label, stats.Mean, stats.StdDev, stats.P50)
	}
	fmt.Println("───────────────────────────────────────────────────────")
	if cm != nil {
		fmt.Print(cm.PrettyPrint())
	}
	fmt.Println("═══════════════════════════════════════════════════════")
	fmt.Printf("Relatórios gerados em:\n")
	fmt.Printf("  %s\n", paths.Summary)
	fmt.Printf("  %s\n", paths.ConfusionMatrix)
	fmt.Printf("  %s\n", paths.Calibration)
	fmt.Printf("  %s\n", paths.FalsePositives)
	fmt.Printf("  %s\n", paths.FalseNegatives)
	fmt.Println("═══════════════════════════════════════════════════════")
}
