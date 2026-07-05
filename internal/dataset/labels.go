// Package dataset gerencia o carregamento, labels e splits do conjunto de avaliação.
package dataset

// Labels de ground truth.
const (
	LabelAuthentic   = "authentic"    // imagem original, não adulterada
	LabelManipulated = "manipulated"  // editada (Photoshop, GIMP, recompressão múltipla, etc.)
	LabelAIGenerated = "ai_generated" // criada integralmente por IA generativa
	LabelPartial     = "partial"      // parcialmente alterada (inpainting, generative fill)
)

// Categorias para análise detalhada de erro por tipo.
const (
	CategoryCameraOriginal      = "camera_original"
	CategoryWhatsAppCompressed  = "whatsapp_compressed"
	CategoryInstagramCompressed = "instagram_compressed"
	CategoryPhotoshopEdit       = "photoshop_edit"
	CategoryGIMPEdit            = "gimp_edit"
	CategoryLightroomEdit       = "lightroom_edit"
	CategoryMultiRecompressed   = "multi_recompressed"
	CategoryEXIFStripped        = "exif_stripped"
	CategoryResized             = "resized"
	CategoryScreenshot          = "screenshot"
	CategoryAIStableDiffusion   = "ai_stable_diffusion"
	CategoryAIMidjourney        = "ai_midjourney"
	CategoryAIDALLE             = "ai_dalle"
	CategoryAIFlux              = "ai_flux"
	CategoryAIImg2Img           = "ai_img2img"
	CategoryPartialInpainting   = "partial_inpainting"
	CategoryPartialGenerative   = "partial_generative_fill"
)

// IsManipulated retorna true se o label representa imagem não-autêntica
// (alvo da detecção — classe POSITIVA para métricas de precisão/recall).
func IsManipulated(label string) bool {
	switch label {
	case LabelManipulated, LabelAIGenerated, LabelPartial:
		return true
	}
	return false
}

// LabelDescription retorna uma descrição legível do label.
func LabelDescription(label string) string {
	switch label {
	case LabelAuthentic:
		return "Imagem autêntica de câmera"
	case LabelManipulated:
		return "Imagem editada/manipulada"
	case LabelAIGenerated:
		return "Imagem gerada por IA"
	case LabelPartial:
		return "Imagem parcialmente alterada"
	}
	return label
}

// AllLabels retorna todos os labels válidos.
func AllLabels() []string {
	return []string{LabelAuthentic, LabelManipulated, LabelAIGenerated, LabelPartial}
}
