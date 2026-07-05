package dataset

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// ManifestEntry é uma linha do arquivo manifest.jsonl.
type ManifestEntry struct {
	// Path é relativo ao diretório base do dataset (onde o manifest está).
	Path     string `json:"path"`
	Label    string `json:"label"`    // authentic | manipulated | ai_generated | partial
	Category string `json:"category"` // camera_original | photoshop_edit | ...
	Notes    string `json:"notes,omitempty"`
	// SHA256 opcional para verificação de integridade.
	SHA256 string `json:"sha256,omitempty"`
}

// Validate verifica se a entrada tem campos obrigatórios válidos.
func (e ManifestEntry) Validate() error {
	if e.Path == "" {
		return fmt.Errorf("campo 'path' obrigatório")
	}
	switch e.Label {
	case LabelAuthentic, LabelManipulated, LabelAIGenerated, LabelPartial:
	default:
		return fmt.Errorf("label inválido %q — esperado: authentic|manipulated|ai_generated|partial", e.Label)
	}
	return nil
}

// LoadManifest lê o arquivo manifest.jsonl e retorna todas as entradas validadas.
// Cada linha do arquivo deve ser um JSON válido representando ManifestEntry.
// Linhas em branco e comentários (iniciando com #) são ignorados.
func LoadManifest(manifestPath string) ([]ManifestEntry, error) {
	f, err := os.Open(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("abrindo manifest %q: %w", manifestPath, err)
	}
	defer f.Close()

	var entries []ManifestEntry
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		var e ManifestEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("manifest linha %d: parse error: %w", lineNum, err)
		}
		if err := e.Validate(); err != nil {
			return nil, fmt.Errorf("manifest linha %d: %w", lineNum, err)
		}
		entries = append(entries, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("lendo manifest: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("manifest vazio: nenhuma entrada encontrada em %q", manifestPath)
	}
	return entries, nil
}

// WriteManifest grava entradas no formato JSONL (uma por linha).
func WriteManifest(path string, entries []ManifestEntry) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("criando manifest %q: %w", path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return fmt.Errorf("escrevendo entrada %q: %w", e.Path, err)
		}
	}
	return nil
}
