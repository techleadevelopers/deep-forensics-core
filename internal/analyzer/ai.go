//go:build !windows

package analyzer

import (
	"bytes"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/PixelAudit/PixelAudit/internal/model"
)

// AIDetector executa inferência ONNX sobre uma CNN treinada para
// detectar imagens geradas por GAN/Diffusion.
type AIDetector struct {
	session   *ort.AdvancedSession
	modelPath string
	version   string
}

// NewAIDetector carrega o modelo ONNX do disco.
// Retorna nil, err se o runtime não estiver disponível.
func NewAIDetector(modelPath string) (*AIDetector, error) {
	if modelPath == "" {
		return nil, errors.New("model path empty")
	}
	if !ort.IsInitialized() {
		ort.SetSharedLibraryPath(sharedLibPath())
		if err := ort.InitializeEnvironment(); err != nil {
			return nil, err
		}
	}

	inputShape := ort.NewShape(1, 3, 224, 224)
	outputShape := ort.NewShape(1, 1)
	inputTensor, err := ort.NewEmptyTensor[float32](inputShape)
	if err != nil {
		return nil, err
	}
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return nil, err
	}
	session, err := ort.NewAdvancedSession(modelPath,
		[]string{"input"}, []string{"output"},
		[]ort.Value{inputTensor}, []ort.Value{outputTensor}, nil)
	if err != nil {
		return nil, err
	}
	return &AIDetector{session: session, version: "gan_detector_v1.2.0"}, nil
}

// Detect roda a inferência e retorna confidence 0..1 (1 = altíssima chance de IA).
func (d *AIDetector) Detect(imgBytes []byte) (*model.AIResult, error) {
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return &model.AIResult{Confidence: 0.2, ModelVersion: d.version}, nil
	}
	tensor := preprocess224(img)

	inputShape := ort.NewShape(1, 3, 224, 224)
	inputT, err := ort.NewTensor(inputShape, tensor)
	if err != nil {
		return nil, err
	}
	outputShape := ort.NewShape(1, 1)
	outputT, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return nil, err
	}
	// Recria session bindings (o session guarda seus tensores)
	sess, err := ort.NewAdvancedSession(d.session.Path(),
		[]string{"input"}, []string{"output"},
		[]ort.Value{inputT}, []ort.Value{outputT}, nil)
	if err != nil {
		return nil, err
	}
	defer sess.Destroy()
	if err := sess.Run(); err != nil {
		return nil, err
	}

	score := float64(outputT.GetData()[0])
	return &model.AIResult{
		IsAIGenerated: score > 0.7,
		Confidence:    score,
		ModelVersion:  d.version,
	}, nil
}

// preprocess224 redimensiona (nearest neighbor) para 224x224 e normaliza para [-1, 1] CHW.
func preprocess224(img image.Image) []float32 {
	const size = 224
	bounds := img.Bounds()
	sx := float64(bounds.Dx()) / size
	sy := float64(bounds.Dy()) / size

	out := make([]float32, 3*size*size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			ox := bounds.Min.X + int(float64(x)*sx)
			oy := bounds.Min.Y + int(float64(y)*sy)
			r, g, b, _ := img.At(ox, oy).RGBA()
			// [-1, 1]
			out[0*size*size+y*size+x] = float32(r)/32767.5 - 1
			out[1*size*size+y*size+x] = float32(g)/32767.5 - 1
			out[2*size*size+y*size+x] = float32(b)/32767.5 - 1
		}
	}
	return out
}

// sharedLibPath retorna o caminho para libonnxruntime, injetável em runtime.
func sharedLibPath() string {
	return "/opt/onnxruntime/lib/libonnxruntime.so"
}
