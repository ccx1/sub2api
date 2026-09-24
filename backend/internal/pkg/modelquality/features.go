package modelquality

import "math"

func countNumbers(numbers []int) []int {
	counts := make([]int, dimension)
	for _, number := range numbers {
		counts[number-1]++
	}
	return counts
}

func hellingerFeature(counts []int) []float64 {
	feature, total := make([]float64, len(counts)), 0.0
	for i, count := range counts {
		feature[i] = float64(count) + alpha
		total += feature[i]
	}
	for i := range feature {
		feature[i] = math.Sqrt(feature[i] / total)
	}
	return feature
}

func orderedFeature(numbers []int) []float64 {
	feature := make([]float64, 0, 74)
	start := 0
	for block := 0; block < 4; block++ {
		size := len(numbers) / 4
		if block < len(numbers)%4 {
			size++
		}
		counts := make([]int, 16)
		for _, number := range numbers[start : start+size] {
			counts[(number-1)*16/dimension]++
		}
		feature = append(feature, hellingerFeature(counts)...)
		start += size
	}
	digits := make([]int, 10)
	for _, number := range numbers {
		digits[number%10]++
	}
	return append(feature, hellingerFeature(digits)...)
}

func standardize(values []float64) []float64 {
	mean := 0.0
	for _, value := range values {
		mean += value / float64(len(values))
	}
	variance := 0.0
	for _, value := range values {
		variance += (value - mean) * (value - mean) / float64(len(values))
	}
	result, scale := make([]float64, len(values)), max(math.Sqrt(variance), 1e-12)
	for i, value := range values {
		result[i] = (value - mean) / scale
	}
	return result
}

func normalized(values []float64) []float64 {
	result, scale := make([]float64, len(values)), max(math.Sqrt(dot(values, values)), 1e-12)
	for i, value := range values {
		result[i] = value / scale
	}
	return result
}

func dot(left, right []float64) float64 {
	result := 0.0
	for i, value := range left {
		result += value * right[i]
	}
	return result
}

func project(feature []float64, basis [][]float64) []float64 {
	result := append([]float64(nil), feature...)
	for _, vector := range basis {
		projection := dot(feature, vector)
		for i := range result {
			result[i] -= projection * vector[i]
		}
	}
	return normalized(result)
}

func (artifact *featureArtifact) transform(feature []float64) []float64 {
	result := make([]float64, len(feature))
	for i, value := range feature {
		result[i] = (value - artifact.Mean[i]) / artifact.Scale[i]
	}
	return result
}

func centroidScores(feature []float64, centroids [][]float64) []float64 {
	scores := make([]float64, len(centroids))
	for i, centroid := range centroids {
		scores[i] = dot(feature, centroid)
	}
	return standardize(scores)
}

func (bank *referenceBank) scoreNumbers(numbers, counts []int) []float64 {
	marginal := &bank.Robust.Hellinger
	feature := marginal.transform(hellingerFeature(counts))
	scores := centroidScores(project(feature, marginal.Basis), marginal.Centroids)
	ordered := &bank.Robust.Ordered
	if ordered.Weight != 0 {
		for i, score := range ordered.scoreOrdered(numbers) {
			scores[i] = (1-ordered.Weight)*scores[i] + ordered.Weight*score
		}
	}
	return scores
}

func (artifact *featureArtifact) scoreOrdered(numbers []int) []float64 {
	feature := artifact.transform(orderedFeature(numbers))
	unit := normalized(feature)
	template := make([]float64, len(artifact.Centroids))
	for i := range template {
		template[i] = math.Inf(-1)
		for _, environment := range artifact.Environments {
			template[i] = max(template[i], dot(unit, environment[i]))
		}
	}
	template = standardize(template)
	nuisance := centroidScores(project(feature, artifact.Basis), artifact.Centroids)
	for i := range template {
		template[i] = 0.5*template[i] + 0.5*nuisance[i]
	}
	return standardize(template)
}
