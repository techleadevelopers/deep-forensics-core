// Comando eval executa o pipeline de verificação forense sobre um dataset anotado
// e gera relatórios de métricas em reports/.
//
// Uso:
//
//	go run ./cmd/eval --dataset ./datasets/manifest.jsonl
//
// Flags disponíveis:
//
//	--dataset      Caminho para o arquivo manifest.jsonl (obrigatório)
//	--base-dir     Diretório base para resolver paths do manifest (padrão: diretório do manifest)
//	--reports-dir  Diretório de saída para relatórios (padrão: ./reports)
//	--threshold    Score de decisão (0–100) acima do qual a imagem é classificada como manipulada (padrão: 50)
//	--workers      Número de goroutines paralelas de inferência (padrão: numCPU, máx 8)
//	--model        Caminho para o modelo ONNX (opcional; omitir desabilita o AI detector)
//	--bins         Número de bins para histograma de calibração (padrão: 10)
//	--verbose      Imprime progresso por imagem
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/verifood/verifood/internal/dataset"
	"github.com/verifood/verifood/internal/evaluation"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ERRO: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// ── flags ──────────────────────────────────────────────────────────────────
	manifestPath := flag.String("dataset", "", "Caminho para manifest.jsonl (obrigatório)")
	reportsDir := flag.String("reports-dir", "reports", "Diretório de saída para relatórios JSON")
	threshold := flag.Float64("threshold", 50.0, "Score de decisão 0–100 (acima = manipulado)")
	workers := flag.Int("workers", 0, "Goroutines paralelas (0 = numCPU, máx 8)")
	modelPath := flag.String("model", "", "Caminho para modelo ONNX (vazio = sem AI detector)")
	nBins := flag.Int("bins", 10, "Número de bins para calibração")
	verbose := flag.Bool("verbose", false, "Imprime progresso por imagem")
	flag.Parse()

	if *manifestPath == "" {
		flag.Usage()
		return fmt.Errorf("--dataset é obrigatório")
	}

	absManifest, err := filepath.Abs(*manifestPath)
	if err != nil {
		return fmt.Errorf("resolvendo caminho do manifest: %w", err)
	}

	absReports, err := filepath.Abs(*reportsDir)
	if err != nil {
		return fmt.Errorf("resolvendo caminho de relatórios: %w", err)
	}

	w := *workers
	if w <= 0 {
		w = runtime.NumCPU()
		if w > 8 {
			w = 8
		}
	}

	fmt.Println("VeriFood — Accuracy Evaluation Harness")
	fmt.Printf("Manifest:   %s\n", absManifest)
	fmt.Printf("Reports:    %s\n", absReports)
	fmt.Printf("Threshold:  %.1f\n", *threshold)
	fmt.Printf("Workers:    %d\n", w)
	if *modelPath != "" {
		fmt.Printf("ONNX Model: %s\n", *modelPath)
	} else {
		fmt.Println("ONNX Model: desabilitado")
	}
	fmt.Println()

	evalStart := time.Now()

	// ── Carregamento de dataset ────────────────────────────────────────────────
	fmt.Printf("Carregando imagens...\n")
	lr, err := dataset.LoadAll(absManifest, w)
	if err != nil {
		return fmt.Errorf("carregando dataset: %w", err)
	}
	fmt.Printf("  Carregadas: %d / %d\n", lr.Loaded, lr.Total)
	if len(lr.Skipped) > 0 {
		fmt.Printf("  Ignoradas:  %d\n", len(lr.Skipped))
		for _, sk := range lr.Skipped {
			fmt.Printf("    SKIP %s — %s\n", sk.Entry.Path, sk.Reason)
		}
	}
	if lr.Loaded == 0 {
		return fmt.Errorf("nenhuma imagem carregada — verifique os paths no manifest")
	}
	fmt.Println()

	stats := lr.Stats()
	fmt.Println("Distribuição do dataset:")
	for label, count := range stats {
		fmt.Printf("  %-20s %d\n", label, count)
	}
	fmt.Println()

	// ── Configuração do runner ─────────────────────────────────────────────────
	runner, err := evaluation.NewRunner(*modelPath)
	if err != nil {
		return fmt.Errorf("inicializando runner: %w", err)
	}
	runner.Workers = w

	if *verbose {
		runner.ProgressFn = func(done, total int) {
			fmt.Printf("\r  Progresso: %d/%d (%.1f%%)", done, total, float64(done)/float64(total)*100)
		}
	} else {
		// Progresso em dot mode
		var lastPct int
		runner.ProgressFn = func(done, total int) {
			pct := done * 10 / total
			if pct > lastPct {
				fmt.Print(".")
				lastPct = pct
			}
		}
	}

	// ── Execução do pipeline ───────────────────────────────────────────────────
	fmt.Printf("Executando pipeline em %d imagens", lr.Loaded)
	ctx := context.Background()
	results := runner.RunAll(ctx, lr.Samples)
	fmt.Println(" ✓")
	fmt.Println()

	// Contagem de erros de pipeline
	var pipelineErrors int
	for _, r := range results {
		if r.Error != "" && r.Result == nil {
			pipelineErrors++
			if *verbose {
				fmt.Printf("  PIPELINE_ERR %s: %s\n", r.Entry.Path, r.Error)
			}
		}
	}
	if pipelineErrors > 0 {
		fmt.Printf("  Erros de pipeline: %d\n\n", pipelineErrors)
	}

	// ── Métricas ───────────────────────────────────────────────────────────────
	fmt.Println("Calculando métricas...")
	metrics := evaluation.ComputeMetrics(results, *threshold)

	cm := evaluation.BinaryConfusionMatrix(results, *threshold)
	cal := evaluation.ComputeCalibration(results, *nBins)
	roc := evaluation.ComputeROC(results)

	// Threshold ótimo da calibração
	recommendedThreshold := *threshold
	if cal != nil && cal.OptimalThreshold > 0 {
		recommendedThreshold = cal.OptimalThreshold
	}

	// ── Geração de relatórios ──────────────────────────────────────────────────
	fmt.Printf("Gerando relatórios em %s...\n", absReports)

	modelInfo := map[string]string{
		"ai_detector": "disabled",
	}
	if *modelPath != "" {
		modelInfo["ai_detector"] = "enabled"
		modelInfo["model_path"] = *modelPath
	}

	summary := &evaluation.EvalSummary{
		SchemaVersion: "1.0",
		GeneratedAt:   time.Now().UTC(),
		Run: evaluation.RunMeta{
			ManifestPath: absManifest,
			TotalSamples: lr.Total,
			LoadedImages: lr.Loaded,
			Skipped:      len(lr.Skipped),
			Threshold:    *threshold,
			Workers:      w,
			AIEnabled:    *modelPath != "",
			DatasetStats: stats,
			DurationMs:   time.Since(evalStart).Milliseconds(),
		},
		Metrics:              metrics,
		RecommendedThreshold: recommendedThreshold,
		ModelInfo:            modelInfo,
	}

	paths, err := evaluation.WriteReports(
		absReports,
		summary,
		cm,
		cal,
		roc,
		results,
		*threshold,
	)
	if err != nil {
		return fmt.Errorf("gerando relatórios: %w", err)
	}

	// ── Output final ───────────────────────────────────────────────────────────
	fmt.Println()
	evaluation.PrintSummary(summary, cm, cal, paths)

	if metrics.Binary.F1 < 0.7 {
		fmt.Println()
		fmt.Println("⚠  F1 abaixo de 0.70 — recomenda-se revisar pesos do fusion ou threshold.")
		fmt.Printf("   Threshold ótimo sugerido: %.1f (F1=%.4f)\n", cal.OptimalThreshold, cal.OptimalF1)
	}

	return nil
}
