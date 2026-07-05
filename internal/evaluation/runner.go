package evaluation

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PixelAudit/PixelAudit/internal/analyzer"
	"github.com/PixelAudit/PixelAudit/internal/dataset"
	"github.com/PixelAudit/PixelAudit/internal/model"
)

// PredictionResult é o resultado do pipeline para uma única imagem.
type PredictionResult struct {
	// Entry é o ground truth da imagem.
	Entry dataset.ManifestEntry
	// Result é o output completo do pipeline (nil se houve erro).
	Result *model.VerificationResult
	// Score é o score de manipulação (0–100) extraído do Result.
	// Convenção: 0 = certamente autêntico, 100 = certamente manipulado.
	Score float64
	// LatencyMs é o tempo total de inferência (todos os analisadores + fusion).
	LatencyMs int64
	// Error contém a mensagem de erro se o pipeline falhou.
	Error string
}

// IsCorrect verifica se a predição está correta para o threshold dado.
func (p *PredictionResult) IsCorrect(threshold float64) bool {
	actualManip := isManipulatedLabel(p.Entry.Label)
	predictedManip := p.Score >= threshold
	return actualManip == predictedManip
}

// IsFalsePositive retorna true se marcou como fraude uma imagem autêntica.
func (p *PredictionResult) IsFalsePositive(threshold float64) bool {
	return !isManipulatedLabel(p.Entry.Label) && p.Score >= threshold
}

// IsFalseNegative retorna true se não detectou fraude em imagem manipulada.
func (p *PredictionResult) IsFalseNegative(threshold float64) bool {
	return isManipulatedLabel(p.Entry.Label) && p.Score < threshold
}

// Runner executa o pipeline de análise forense em lote sobre um dataset.
// Não depende de Postgres, Redis, NATS ou S3 — apenas dos analisadores.
type Runner struct {
	meta   *analyzer.MetadataAnalyzer
	ela    *analyzer.ELAAnalyzer
	ai     *analyzer.AIDetector // pode ser nil se modelo ONNX não disponível
	freq   *analyzer.FrequencyAnalyzer
	fusion *analyzer.Fusion
	// Workers controla o paralelismo de inferência. 0 = runtime.NumCPU().
	Workers int
	// ProgressFn é chamado após cada imagem processada (thread-safe). Pode ser nil.
	ProgressFn func(done, total int)
}

// NewRunner constrói um Runner com os analisadores padrão.
// aiModelPath pode ser "" para desabilitar o detector ONNX.
func NewRunner(aiModelPath string) (*Runner, error) {
	meta := analyzer.NewMetadataAnalyzer()
	ela := analyzer.NewELAAnalyzer(0.02)
	freq := analyzer.NewFrequencyAnalyzer()
	fusion := analyzer.NewFusion(analyzer.DefaultWeights)

	var ai *analyzer.AIDetector
	if aiModelPath != "" {
		var err error
		ai, err = analyzer.NewAIDetector(aiModelPath)
		if err != nil {
			// Não fatal — eval continua com 3 analisadores
			fmt.Printf("AVISO: AIDetector desabilitado (%v). Continuando sem inferência ONNX.\n", err)
		}
	}

	return &Runner{
		meta:   meta,
		ela:    ela,
		ai:     ai,
		freq:   freq,
		fusion: fusion,
	}, nil
}

// NewRunnerWithAnalyzers constrói um Runner com analisadores já instanciados
// (útil para testes ou para passar analisadores customizados).
func NewRunnerWithAnalyzers(
	meta *analyzer.MetadataAnalyzer,
	ela *analyzer.ELAAnalyzer,
	ai *analyzer.AIDetector,
	freq *analyzer.FrequencyAnalyzer,
	weights analyzer.Weights,
) *Runner {
	return &Runner{
		meta:   meta,
		ela:    ela,
		ai:     ai,
		freq:   freq,
		fusion: analyzer.NewFusion(weights),
	}
}

// RunAll executa o pipeline em todas as amostras e retorna os resultados.
// O contexto pode ser cancelado para interromper o processamento.
func (r *Runner) RunAll(ctx context.Context, samples []dataset.Sample) []PredictionResult {
	workers := r.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
		if workers > 8 {
			workers = 8 // limita para não sobrecarregar memória com imagens grandes
		}
	}

	type job struct {
		idx    int
		sample dataset.Sample
	}

	jobs := make(chan job, len(samples))
	results := make([]PredictionResult, len(samples))

	var done atomic.Int64
	total := len(samples)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				select {
				case <-ctx.Done():
					results[j.idx] = PredictionResult{
						Entry: j.sample.Entry,
						Error: ctx.Err().Error(),
					}
					continue
				default:
				}
				results[j.idx] = r.runOne(j.sample)
				n := done.Add(1)
				if r.ProgressFn != nil {
					r.ProgressFn(int(n), total)
				}
			}
		}()
	}

	for i, s := range samples {
		jobs <- job{idx: i, sample: s}
	}
	close(jobs)
	wg.Wait()

	return results
}

// RunOne executa o pipeline em uma única amostra. Exportado para uso em testes.
func (r *Runner) RunOne(s dataset.Sample) PredictionResult {
	return r.runOne(s)
}

func (r *Runner) runOne(s dataset.Sample) PredictionResult {
	start := time.Now()

	type box struct {
		m *model.MetadataResult
		e *model.ELAResult
		a *model.AIResult
		f *model.FrequencyResult
	}
	b := box{}

	var mu sync.Mutex
	var wg sync.WaitGroup
	var pipelineErr string

	wg.Add(4)

	go func() {
		defer wg.Done()
		m, err := r.meta.Analyze(s.ImageData)
		if err != nil {
			mu.Lock()
			pipelineErr += fmt.Sprintf("metadata:%v; ", err)
			mu.Unlock()
			return
		}
		mu.Lock()
		b.m = m
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		e, err := r.ela.Analyze(s.ImageData)
		if err != nil {
			mu.Lock()
			pipelineErr += fmt.Sprintf("ela:%v; ", err)
			mu.Unlock()
			return
		}
		mu.Lock()
		b.e = e
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		if r.ai == nil {
			return
		}
		a, err := r.ai.Detect(s.ImageData)
		if err != nil {
			mu.Lock()
			pipelineErr += fmt.Sprintf("ai:%v; ", err)
			mu.Unlock()
			return
		}
		mu.Lock()
		b.a = a
		mu.Unlock()
	}()

	go func() {
		defer wg.Done()
		f, err := r.freq.Analyze(s.ImageData)
		if err != nil {
			mu.Lock()
			pipelineErr += fmt.Sprintf("frequency:%v; ", err)
			mu.Unlock()
			return
		}
		mu.Lock()
		b.f = f
		mu.Unlock()
	}()

	wg.Wait()
	latencyMs := time.Since(start).Milliseconds()

	// Se todos os analisadores falharam, retorna erro
	if b.m == nil && b.e == nil && b.a == nil && b.f == nil {
		return PredictionResult{
			Entry:     s.Entry,
			LatencyMs: latencyMs,
			Error:     "todos os analisadores falharam: " + pipelineErr,
		}
	}

	res := r.fusion.Combine(b.m, b.e, b.a, b.f)
	res.ProcessingTimeMs = int(latencyMs)

	// Score: Confidence já é 0–100 no VerificationResult
	score := res.Confidence

	return PredictionResult{
		Entry:     s.Entry,
		Result:    res,
		Score:     score,
		LatencyMs: latencyMs,
		Error:     pipelineErr,
	}
}

// FilterFalsePositives retorna apenas os false positives dado um threshold.
func FilterFalsePositives(results []PredictionResult, threshold float64) []PredictionResult {
	var out []PredictionResult
	for _, r := range results {
		if r.IsFalsePositive(threshold) {
			out = append(out, r)
		}
	}
	return out
}

// FilterFalseNegatives retorna apenas os false negatives dado um threshold.
func FilterFalseNegatives(results []PredictionResult, threshold float64) []PredictionResult {
	var out []PredictionResult
	for _, r := range results {
		if r.IsFalseNegative(threshold) {
			out = append(out, r)
		}
	}
	return out
}
