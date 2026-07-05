//go:build windows

package analyzer

import (
	"errors"

	"github.com/verifood/verifood/internal/model"
)

type AIDetector struct{}

func NewAIDetector(modelPath string) (*AIDetector, error) {
	return nil, errors.New("onnxruntime detector is not supported on windows builds")
}

func (d *AIDetector) Detect(imgBytes []byte) (*model.AIResult, error) {
	return nil, errors.New("onnxruntime detector is not supported on windows builds")
}
