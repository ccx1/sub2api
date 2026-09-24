// Package modelquality compares numerical output fingerprints against a finite
// reference bank. Candidate probabilities are relative rankings, not proof of
// model identity or intelligence. See NOTICE for the upstream source and license.
package modelquality

import (
	"errors"
	"math"
	"slices"
	"sort"
)

const BankVersion = "modeltrace-55a2e4a"

type Sample struct {
	Text          string `json:"text"`
	ExpectedCount int    `json:"expected_count"`
}

type Candidate struct {
	Model       string  `json:"model"`
	DisplayName string  `json:"display_name"`
	Probability float64 `json:"probability"`
	Similarity  float64 `json:"similarity"`
	Score       float64 `json:"score"`
}

type Diagnostic struct {
	Index         int    `json:"index"`
	ParsedNumbers int    `json:"parsed_numbers"`
	Accepted      bool   `json:"accepted"`
	Reason        string `json:"reason,omitempty"`
}

type Result struct {
	Candidate   string       `json:"candidate"`
	Probability float64      `json:"probability"`
	Similarity  float64      `json:"similarity"`
	Margin      float64      `json:"margin"`
	UsedOutputs int          `json:"used_outputs"`
	Candidates  []Candidate  `json:"candidates"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Analyze preserves uncertain rankings for the caller to apply its own policy.
// Invalid samples never contribute to scoring or calibration.
func Analyze(outputs []Sample) (Result, error) {
	if len(outputs) == 0 || len(outputs) > 3 {
		return Result{}, errors.New("fingerprint analysis requires one to three samples")
	}
	bank, err := loadBank()
	if err != nil {
		return Result{}, err
	}
	result := Result{Diagnostics: make([]Diagnostic, 0, len(outputs))}
	scores, pooled := make([]float64, len(bank.Models)), make([]int, dimension)
	var accepted [][]int
	for index, sample := range outputs {
		numbers, parseErr := parseSample(sample)
		if parseErr == nil && containsSample(accepted, numbers) {
			parseErr = errors.New("duplicate sample does not provide independent evidence")
		}
		diagnostic := Diagnostic{Index: index, ParsedNumbers: len(numbers), Accepted: parseErr == nil}
		if parseErr != nil {
			diagnostic.Reason = parseErr.Error()
		} else {
			counts := countNumbers(numbers)
			for i, score := range bank.scoreNumbers(numbers, counts) {
				scores[i] += score
			}
			for i, count := range counts {
				pooled[i] += count
			}
			result.UsedOutputs++
			accepted = append(accepted, numbers)
		}
		result.Diagnostics = append(result.Diagnostics, diagnostic)
	}
	if result.UsedOutputs == 0 {
		return result, errors.New("no valid fingerprint samples")
	}
	result.Candidates = bank.rank(scores, pooled, result.UsedOutputs)
	top := result.Candidates[0]
	result.Candidate, result.Probability, result.Similarity = top.Model, top.Probability, top.Similarity
	result.Margin = top.Probability - result.Candidates[1].Probability
	return result, nil
}

func containsSample(samples [][]int, numbers []int) bool {
	for _, sample := range samples {
		if slices.Equal(sample, numbers) {
			return true
		}
	}
	return false
}

func (bank *referenceBank) rank(scores []float64, pooled []int, used int) []Candidate {
	for i := range scores {
		scores[i] /= float64(used)
	}
	beta := bank.Calibration[calibrationKey(used)].Beta
	probabilities := softmax(scores, beta)
	result := make([]Candidate, len(scores))
	for i, model := range bank.Models {
		result[i] = Candidate{
			Model: model.ID, DisplayName: model.DisplayName,
			Probability: probabilities[i], Similarity: jsSimilarity(pooled, model.Counts), Score: scores[i],
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Probability > result[j].Probability })
	return result
}

func calibrationKey(used int) string {
	return []string{"", "1", "2", "3"}[min(used, 3)]
}

func softmax(scores []float64, beta float64) []float64 {
	maximum := scores[0]
	for _, score := range scores {
		maximum = math.Max(maximum, score)
	}
	weights, total := make([]float64, len(scores)), 0.0
	for i, score := range scores {
		weights[i] = math.Exp(beta * (score - maximum))
		total += weights[i]
	}
	for i := range weights {
		weights[i] /= total
	}
	return weights
}

func jsSimilarity(left, right []int) float64 {
	leftTotal, rightTotal := 0.0, alpha*dimension
	for i := range left {
		leftTotal += float64(left[i])
		rightTotal += float64(right[i])
	}
	divergence := 0.0
	for i, count := range left {
		p, q := float64(count)/leftTotal, (float64(right[i])+alpha)/rightTotal
		midpoint := (p + q) / 2
		if p > 0 {
			divergence += p * math.Log(p/midpoint)
		}
		divergence += q * math.Log(q/midpoint)
	}
	return max(0, 1-math.Sqrt(max(0, divergence/2)/math.Ln2))
}
