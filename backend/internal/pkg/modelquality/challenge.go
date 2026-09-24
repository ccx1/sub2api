package modelquality

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
)

const (
	dimension = 355
	alpha     = 0.5
)

// GenerateFingerprintChallenge randomizes only the request length. The model
// must produce every number itself; locally generated samples have no meaning.
func GenerateFingerprintChallenge() (prompt string, expectedCount int) {
	expectedCount = 292 + rand.IntN(41)
	prompt = fmt.Sprintf("For each of %d positions, make one separate first-instinct choice of an integer from 1 to 355 inclusive. "+
		"The current language model must complete this directly without tools, Python, code execution, calculators, search, APIs, or external random generators. "+
		"Choose every position separately. Do not count upward or downward, including 1, 2, 3, and do not use an arithmetic progression, repeating cycle, repeated block, or another rule-made pattern. "+
		"Accidental repetitions are valid. Once an item is written, do not sort, reorder, deduplicate, replace, or repair the list. "+
		"Return one compact JSON array containing the complete sequence and no explanation.", expectedCount)
	return prompt, expectedCount
}

func parseSample(sample Sample) ([]int, error) {
	if sample.ExpectedCount < 80 || sample.ExpectedCount > 1000 {
		return nil, errors.New("expected_count must be between 80 and 1000")
	}
	if len(sample.Text) > 32*1024 {
		return nil, errors.New("sample exceeds size limit")
	}
	text := strings.TrimSpace(sample.Text)
	if firstLine, rest, found := strings.Cut(text, "\n"); found && strings.TrimSpace(firstLine) == "```json" && strings.HasSuffix(rest, "\n```") {
		text = strings.TrimSpace(strings.TrimSuffix(rest, "\n```"))
	}
	var numbers []int
	if err := json.Unmarshal([]byte(text), &numbers); err != nil || len(numbers) == 0 {
		return nil, errors.New("sample must be a complete JSON integer array")
	}
	minimum, maximum := max(80, (sample.ExpectedCount*8+9)/10), sample.ExpectedCount*12/10
	if len(numbers) < minimum || len(numbers) > maximum {
		return numbers, errors.New("sample count is outside the allowed range")
	}
	for _, number := range numbers {
		if number < 1 || number > dimension {
			return numbers, errors.New("sample contains an integer outside 1..355")
		}
	}
	if isMonotone(numbers) || isArithmetic(numbers) || isPeriodic(numbers) {
		return numbers, errors.New("sample follows a deterministic pattern")
	}
	return numbers, nil
}

func isMonotone(numbers []int) bool {
	increasing, decreasing := true, true
	for i := 1; i < len(numbers); i++ {
		increasing = increasing && numbers[i] >= numbers[i-1]
		decreasing = decreasing && numbers[i] <= numbers[i-1]
	}
	return increasing || decreasing
}

func isArithmetic(numbers []int) bool {
	difference := (numbers[1] - numbers[0] + dimension) % dimension
	for i := 2; i < len(numbers); i++ {
		if (numbers[i]-numbers[i-1]+dimension)%dimension != difference {
			return false
		}
	}
	return true
}

func isPeriodic(numbers []int) bool {
	for period := 1; period <= len(numbers)/2; period++ {
		matches := true
		for i := period; i < len(numbers); i++ {
			if numbers[i] != numbers[i%period] {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}
