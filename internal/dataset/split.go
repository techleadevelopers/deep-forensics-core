package dataset

import (
	"math/rand"
	"sort"
)

// SplitOptions configura o comportamento do split.
type SplitOptions struct {
	// TestRatio é a fração das amostras destinada ao conjunto de teste (ex: 0.2 = 20%).
	TestRatio float64
	// Seed para reprodutibilidade.
	Seed int64
	// Stratified mantém a proporção de labels entre train e test.
	Stratified bool
}

// DefaultSplitOptions retorna opções padrão (80/20, seed fixo, estratificado).
func DefaultSplitOptions() SplitOptions {
	return SplitOptions{
		TestRatio:  0.2,
		Seed:       42,
		Stratified: true,
	}
}

// Split divide as amostras em conjuntos de treinamento e teste.
// Quando Stratified=true, preserva a proporção de cada label em ambos os conjuntos.
func Split(samples []Sample, opts SplitOptions) (train, test []Sample) {
	rng := rand.New(rand.NewSource(opts.Seed))

	if opts.Stratified {
		return stratifiedSplit(samples, opts.TestRatio, rng)
	}

	shuffled := make([]Sample, len(samples))
	copy(shuffled, samples)
	rng.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	cutoff := int(float64(len(shuffled)) * (1 - opts.TestRatio))
	return shuffled[:cutoff], shuffled[cutoff:]
}

// stratifiedSplit divide por label para manter proporção.
func stratifiedSplit(samples []Sample, testRatio float64, rng *rand.Rand) (train, test []Sample) {
	// Agrupa por label
	byLabel := make(map[string][]Sample)
	for _, s := range samples {
		byLabel[s.Entry.Label] = append(byLabel[s.Entry.Label], s)
	}

	// Ordena os labels para determinismo
	labels := make([]string, 0, len(byLabel))
	for l := range byLabel {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	for _, label := range labels {
		group := byLabel[label]
		rng.Shuffle(len(group), func(i, j int) {
			group[i], group[j] = group[j], group[i]
		})
		cutoff := len(group) - int(float64(len(group))*testRatio)
		if cutoff < 1 {
			cutoff = 1
		}
		train = append(train, group[:cutoff]...)
		test = append(test, group[cutoff:]...)
	}

	// Embaralha os conjuntos finais para que não venham agrupados por label
	rng.Shuffle(len(train), func(i, j int) { train[i], train[j] = train[j], train[i] })
	rng.Shuffle(len(test), func(i, j int) { test[i], test[j] = test[j], test[i] })

	return train, test
}

// KFold divide as amostras em k folds para cross-validation.
// Retorna os índices de cada fold (não copia dados).
func KFold(n, k int, seed int64) [][]int {
	rng := rand.New(rand.NewSource(seed))
	indices := make([]int, n)
	for i := range indices {
		indices[i] = i
	}
	rng.Shuffle(n, func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })

	folds := make([][]int, k)
	for i, idx := range indices {
		folds[i%k] = append(folds[i%k], idx)
	}
	return folds
}
