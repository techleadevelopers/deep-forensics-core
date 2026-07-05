package dataset

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// Sample é uma imagem carregada do disco com seus metadados de ground truth.
type Sample struct {
	Entry     ManifestEntry
	ImageData []byte
	// AbsPath é o caminho absoluto no disco (para logs/relatórios).
	AbsPath string
}

// LoadResult agrega amostras carregadas e eventuais avisos de arquivos ausentes.
type LoadResult struct {
	Samples  []Sample
	Skipped  []SkippedEntry
	Total    int
	Loaded   int
}

// SkippedEntry registra uma entrada que não pôde ser carregada.
type SkippedEntry struct {
	Entry  ManifestEntry
	Reason string
}

// LoadAll carrega todas as imagens referenciadas no manifest.
// baseDir é o diretório base para resolver os paths relativos do manifest.
// workers controla a paralelização do I/O; <= 0 usa 8.
func LoadAll(manifestPath string, workers int) (*LoadResult, error) {
	entries, err := LoadManifest(manifestPath)
	if err != nil {
		return nil, err
	}

	baseDir := filepath.Dir(manifestPath)

	if workers <= 0 {
		workers = 8
	}

	type job struct {
		entry ManifestEntry
	}
	type result struct {
		sample  *Sample
		skipped *SkippedEntry
	}

	jobs := make(chan job, len(entries))
	results := make(chan result, len(entries))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				absPath := filepath.Join(baseDir, filepath.FromSlash(j.entry.Path))
				data, err := os.ReadFile(absPath)
				if err != nil {
					results <- result{skipped: &SkippedEntry{Entry: j.entry, Reason: err.Error()}}
					continue
				}
				// Verificação de integridade opcional
				if j.entry.SHA256 != "" {
					sum := sha256.Sum256(data)
					got := hex.EncodeToString(sum[:])
					if got != j.entry.SHA256 {
						results <- result{skipped: &SkippedEntry{
							Entry:  j.entry,
							Reason: fmt.Sprintf("sha256 mismatch: expected %s, got %s", j.entry.SHA256, got),
						}}
						continue
					}
				}
				results <- result{sample: &Sample{
					Entry:     j.entry,
					ImageData: data,
					AbsPath:   absPath,
				}}
			}
		}()
	}

	for _, e := range entries {
		jobs <- job{entry: e}
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	lr := &LoadResult{Total: len(entries)}
	for r := range results {
		if r.sample != nil {
			lr.Samples = append(lr.Samples, *r.sample)
			lr.Loaded++
		} else {
			lr.Skipped = append(lr.Skipped, *r.skipped)
		}
	}

	return lr, nil
}

// LoadSingle carrega uma única imagem do disco sem manifest.
func LoadSingle(absPath string) ([]byte, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("carregando imagem %q: %w", absPath, err)
	}
	return data, nil
}

// Stats retorna contagens por label do resultado de carga.
func (lr *LoadResult) Stats() map[string]int {
	counts := make(map[string]int)
	for _, s := range lr.Samples {
		counts[s.Entry.Label]++
	}
	return counts
}

// ByLabel filtra amostras por label.
func (lr *LoadResult) ByLabel(label string) []Sample {
	var out []Sample
	for _, s := range lr.Samples {
		if s.Entry.Label == label {
			out = append(out, s)
		}
	}
	return out
}

// ByCategory filtra amostras por categoria.
func (lr *LoadResult) ByCategory(category string) []Sample {
	var out []Sample
	for _, s := range lr.Samples {
		if s.Entry.Category == category {
			out = append(out, s)
		}
	}
	return out
}

// atomicCounter é um helper de contagem thread-safe para progresso.
type atomicCounter struct{ n atomic.Int64 }

func (c *atomicCounter) Inc() int64 { return c.n.Add(1) }
func (c *atomicCounter) Get() int64 { return c.n.Load() }
