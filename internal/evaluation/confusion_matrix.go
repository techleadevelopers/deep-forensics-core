package evaluation

import "fmt"

// ConfusionMatrix representa a matriz de confusão completa com rótulos.
type ConfusionMatrix struct {
	// Classes é a lista ordenada de labels (linhas = actual, colunas = predicted).
	// Para classificação binária: ["authentic", "manipulated"].
	Classes []string `json:"classes"`

	// Matrix[i][j] = número de amostras com actual=Classes[i] e predicted=Classes[j].
	Matrix [][]int `json:"matrix"`

	// Totais por linha (actual counts)
	ActualTotals []int `json:"actual_totals"`

	// Totais por coluna (predicted counts)
	PredictedTotals []int `json:"predicted_totals"`

	// PerClassMetrics agrupa métricas por classe no formato multiclass.
	PerClassMetrics []PerClassEntry `json:"per_class_metrics"`
}

// PerClassEntry guarda métricas para uma classe específica (one-vs-rest).
type PerClassEntry struct {
	Class     string  `json:"class"`
	TP        int     `json:"tp"`
	FP        int     `json:"fp"`
	FN        int     `json:"fn"`
	TN        int     `json:"tn"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
	Support   int     `json:"support"` // total de amostras desta classe
}

// BinaryConfusionMatrix constrói a matriz binária a partir dos resultados.
// Classes: [0] = "authentic" (negativo), [1] = "manipulated" (positivo).
func BinaryConfusionMatrix(results []PredictionResult, threshold float64) *ConfusionMatrix {
	classes := []string{"authentic", "manipulated"}
	n := len(classes)

	matrix := make([][]int, n)
	for i := range matrix {
		matrix[i] = make([]int, n)
	}

	for i := range results {
		r := &results[i]
		if r.Error != "" {
			continue
		}
		// actual index
		actual := 0
		if isManipulatedLabel(r.Entry.Label) {
			actual = 1
		}
		// predicted index
		predicted := 0
		if r.Score >= threshold {
			predicted = 1
		}
		matrix[actual][predicted]++
	}

	// Totais
	actualTotals := make([]int, n)
	predictedTotals := make([]int, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			actualTotals[i] += matrix[i][j]
			predictedTotals[j] += matrix[i][j]
		}
	}

	// Per-class metrics (one-vs-rest)
	total := 0
	for i := range actualTotals {
		total += actualTotals[i]
	}

	perClass := make([]PerClassEntry, n)
	for i, cls := range classes {
		tp := matrix[i][i]
		fp := predictedTotals[i] - tp
		fn := actualTotals[i] - tp
		tn := total - tp - fp - fn

		prec := 0.0
		if tp+fp > 0 {
			prec = float64(tp) / float64(tp+fp)
		}
		rec := 0.0
		if tp+fn > 0 {
			rec = float64(tp) / float64(tp+fn)
		}
		f1 := 0.0
		if prec+rec > 0 {
			f1 = 2 * prec * rec / (prec + rec)
		}

		perClass[i] = PerClassEntry{
			Class:     cls,
			TP:        tp,
			FP:        fp,
			FN:        fn,
			TN:        tn,
			Precision: roundTo(prec, 4),
			Recall:    roundTo(rec, 4),
			F1:        roundTo(f1, 4),
			Support:   actualTotals[i],
		}
	}

	return &ConfusionMatrix{
		Classes:         classes,
		Matrix:          matrix,
		ActualTotals:    actualTotals,
		PredictedTotals: predictedTotals,
		PerClassMetrics: perClass,
	}
}

// PrettyPrint retorna uma representação textual da matriz para logs.
func (cm *ConfusionMatrix) PrettyPrint() string {
	out := "Confusion Matrix (actual→ rows, predicted↓ cols):\n"
	out += fmt.Sprintf("%-20s", "")
	for _, c := range cm.Classes {
		out += fmt.Sprintf("%-20s", "pred:"+c)
	}
	out += "\n"
	for i, cls := range cm.Classes {
		out += fmt.Sprintf("%-20s", "act:"+cls)
		for j := range cm.Classes {
			out += fmt.Sprintf("%-20d", cm.Matrix[i][j])
		}
		out += fmt.Sprintf("| total=%d\n", cm.ActualTotals[i])
	}
	return out
}
