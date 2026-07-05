// Package analyzer contém os 4 analisadores forenses do PixelAudit
// (metadata, ELA, IA/ONNX, frequência) e a lógica de score fusion.
package analyzer

import (
	"bytes"
	"strings"
	"time"

	exif "github.com/dsoprea/go-exif/v3"

	"github.com/PixelAudit/PixelAudit/internal/model"
)

// MetadataAnalyzer inspeciona EXIF/XMP/IPTC procurando por rastros de edição.
type MetadataAnalyzer struct {
	SuspiciousSoftware []string
}

// NewMetadataAnalyzer constrói o analisador com a blacklist padrão.
func NewMetadataAnalyzer() *MetadataAnalyzer {
	return &MetadataAnalyzer{
		SuspiciousSoftware: []string{
			"Photoshop", "GIMP", "Lightroom", "Pixelmator", "Affinity Photo",
			"Midjourney", "DALL-E", "Stable Diffusion", "SDXL", "Firefly",
			"Flux", "Runway", "Leonardo",
		},
	}
}

// Analyze retorna um MetadataResult com confidence entre 0..1 (quanto maior, mais suspeito).
func (m *MetadataAnalyzer) Analyze(image []byte) (*model.MetadataResult, error) {
	res := &model.MetadataResult{Confidence: 0.1}
	rawExif, err := exif.SearchAndExtractExif(image)
	if err != nil {
		// Ausência total de EXIF é sinal fraco de manipulação/screenshot.
		res.Reasons = append(res.Reasons, "no_exif_found")
		res.Confidence = 0.35
		return res, nil
	}

	entries, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		return res, nil
	}

	var software, make, modelName, dtOrig, dtDigi, comment string
	for _, e := range entries {
		switch e.TagName {
		case "Software":
			software = toString(e.Value)
		case "Make":
			make = toString(e.Value)
		case "Model":
			modelName = toString(e.Value)
		case "DateTimeOriginal":
			dtOrig = toString(e.Value)
		case "DateTimeDigitized":
			dtDigi = toString(e.Value)
		case "UserComment", "ImageDescription":
			comment = toString(e.Value)
		case "GPSLatitude":
			res.GPSPresent = true
		}
	}

	res.Software = software
	res.Camera = strings.TrimSpace(make + " " + modelName)
	if t, err := time.Parse("2006:01:02 15:04:05", dtOrig); err == nil {
		res.DateTimeOriginal = t
	}

	// Regra 1: software suspeito
	for _, sw := range m.SuspiciousSoftware {
		if strings.Contains(strings.ToLower(software), strings.ToLower(sw)) {
			res.Suspicious = true
			res.Confidence = 0.9
			res.Reasons = append(res.Reasons, "suspicious_software:"+sw)
			return res, nil
		}
	}

	// Regra 2: comentário mencionando IA
	if strings.Contains(strings.ToLower(comment), "ai") ||
		strings.Contains(strings.ToLower(comment), "generated") ||
		strings.Contains(strings.ToLower(comment), "prompt") {
		res.Suspicious = true
		res.Confidence = 0.85
		res.Reasons = append(res.Reasons, "ai_marker_in_comment")
		return res, nil
	}

	// Regra 3: inconsistência de datas
	if dtOrig != "" && dtDigi != "" && dtOrig != dtDigi {
		res.Suspicious = true
		res.Confidence = 0.7
		res.Reasons = append(res.Reasons, "datetime_mismatch")
		return res, nil
	}

	return res, nil
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	}
	if b, ok := v.([]byte); ok {
		return strings.TrimSpace(string(bytes.Trim(b, "\x00")))
	}
	return ""
}
