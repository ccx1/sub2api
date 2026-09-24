package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"strings"
)

type codexQualityChallenge struct {
	Prompt        string
	ExpectedCount int
	Kind          string
	expected      codexQualityCapabilityAnswer
}

type codexQualityCapabilityAnswer struct {
	Selected    []int  `json:"selected"`
	Transformed []int  `json:"transformed"`
	Checksum    int    `json:"checksum"`
	Flags       []bool `json:"flags"`
	Final       int    `json:"final"`
}

type codexQualityCapabilityCheck struct {
	valid, matches bool
}

type codexQualityCapabilityInputs struct {
	candidates, transform, digits           []int
	minimum, excludeDivisor, offset, modulo int
	conditions                              [][3]bool
	start, multiplier, addend, divisor      int
}

// 基本能力冒烟只检查确定性任务，不能据此证明模型身份或衡量智商。
func newCodexQualityCapabilityChallenge() codexQualityChallenge {
	rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	return codexQualityCapabilityChallengeWithRand(rng)
}

func codexQualityCapabilityChallengeWithRand(rng *rand.Rand) codexQualityChallenge {
	in := codexQualityCapabilityInputs{
		minimum: 8 + rng.IntN(8), excludeDivisor: 2 + rng.IntN(4),
		offset: 1 + rng.IntN(7), modulo: 11 + rng.IntN(9),
		start: 3 + rng.IntN(17), multiplier: 2 + rng.IntN(4),
		addend: 1 + rng.IntN(11), divisor: 2 + rng.IntN(4),
	}
	for range 7 {
		in.candidates = append(in.candidates, 1+rng.IntN(30))
	}
	for range 5 {
		in.transform = append(in.transform, rng.IntN(19)-9)
		in.digits = append(in.digits, rng.IntN(10))
	}
	for range 4 {
		in.conditions = append(in.conditions, [3]bool{rng.IntN(2) == 1, rng.IntN(2) == 1, rng.IntN(2) == 1})
	}
	return codexQualityChallenge{Prompt: codexQualityCapabilityPrompt(in), ExpectedCount: 5,
		Kind: "capability", expected: codexQualityCapabilityExpected(in)}
}

func codexQualityCapabilityExpected(in codexQualityCapabilityInputs) codexQualityCapabilityAnswer {
	answer := codexQualityCapabilityAnswer{Selected: []int{}, Transformed: []int{}, Flags: []bool{}}
	for _, value := range in.candidates {
		if value >= in.minimum && value%in.excludeDivisor != 0 {
			answer.Selected = append(answer.Selected, value)
		}
	}
	for i := len(in.transform) - 1; i >= 0; i-- {
		answer.Transformed = append(answer.Transformed, 3*in.transform[i]-in.offset)
	}
	for i, value := range in.digits {
		answer.Checksum += (i + 1) * value
	}
	answer.Checksum %= in.modulo
	for _, row := range in.conditions {
		answer.Flags = append(answer.Flags, row[0] && !row[1] || row[2])
	}
	answer.Final = in.start*in.multiplier + in.addend
	if answer.Final%in.divisor == 0 {
		answer.Final /= in.divisor
	} else {
		answer.Final -= 2
	}
	return answer
}

func codexQualityCapabilityPrompt(in codexQualityCapabilityInputs) string {
	return fmt.Sprintf(`Solve these five independent tasks. Return only one JSON object with exactly the keys selected, transformed, checksum, flags, final. Use integer arrays for selected/transformed, an array of JSON booleans for flags, and integers for checksum/final. No tools, explanation, or extra keys.
1. selected: From %v, keep values >= %d that are NOT divisible by %d. Preserve order and duplicates; return [] if none qualify.
2. transformed: For each value x in %v, compute 3*x-%d, then reverse the resulting array.
3. checksum: For digits %v with positions starting at 1, compute sum(position*digit) modulo %d.
4. flags: Each row is [A,B,C]. For rows %v, evaluate (A AND NOT B) OR C, keeping row order.
5. final: Start with %d, multiply by %d, then add %d. If this result is divisible by %d, divide by %d; otherwise subtract 2.`,
		codexQualityCapabilityJSON(in.candidates), in.minimum, in.excludeDivisor, codexQualityCapabilityJSON(in.transform), in.offset,
		codexQualityCapabilityJSON(in.digits), in.modulo, codexQualityCapabilityJSON(in.conditions), in.start, in.multiplier, in.addend, in.divisor, in.divisor)
}

func codexQualityCapabilityJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func scoreCodexQualityCapability(challenge codexQualityChallenge, text string) (float64, error) {
	if challenge.Kind != "capability" || challenge.ExpectedCount != 5 {
		return 0, errors.New("invalid capability challenge")
	}
	actual, err := decodeCodexQualityCapabilityAnswer(text)
	if err != nil {
		return 0, err
	}
	for key := range actual {
		switch key {
		case "selected", "transformed", "checksum", "flags", "final":
		default:
			return 0, errors.New("capability response contains unknown answer")
		}
	}
	for _, key := range []string{"selected", "transformed", "checksum", "flags", "final"} {
		if _, ok := actual[key]; !ok {
			return 0, errors.New("capability response is missing an answer")
		}
	}
	expected := challenge.expected
	checks := []codexQualityCapabilityCheck{
		compareCodexQualityCapabilityValues(actual["selected"], expected.Selected),
		compareCodexQualityCapabilityValues(actual["transformed"], expected.Transformed),
		compareCodexQualityCapabilityInteger(actual["checksum"], expected.Checksum),
		compareCodexQualityCapabilityValues(actual["flags"], expected.Flags),
		compareCodexQualityCapabilityInteger(actual["final"], expected.Final),
	}
	correct := 0
	for _, check := range checks {
		if !check.valid {
			return 0, errors.New("capability response contains an invalid answer type")
		}
		if check.matches {
			correct++
		}
	}
	return float64(correct) * 100 / float64(challenge.ExpectedCount), nil
}

func compareCodexQualityCapabilityValues[T comparable](raw json.RawMessage, expected []T) codexQualityCapabilityCheck {
	// JSON null 在基本类型中会被解码为零值，指针元素用于保留并拒绝这种无效样本。
	var values []*T
	if json.Unmarshal(raw, &values) != nil || values == nil || slices.Contains(values, nil) {
		return codexQualityCapabilityCheck{}
	}
	matches := slices.EqualFunc(values, expected, func(value *T, want T) bool { return *value == want })
	return codexQualityCapabilityCheck{valid: true, matches: matches}
}

func compareCodexQualityCapabilityInteger(raw json.RawMessage, expected int) codexQualityCapabilityCheck {
	var value *int
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return codexQualityCapabilityCheck{}
	}
	return codexQualityCapabilityCheck{valid: true, matches: *value == expected}
}

func decodeCodexQualityCapabilityAnswer(text string) (map[string]json.RawMessage, error) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > 16<<10 || codexQualityCapabilityRefused(text) {
		return nil, errors.New("capability response is empty, refused, or oversized")
	}
	if strings.HasPrefix(text, "```") {
		header, body, found := strings.Cut(text, "\n")
		if !found || !strings.EqualFold(strings.TrimSpace(header), "```json") && strings.TrimSpace(header) != "```" || !strings.HasSuffix(body, "```") {
			return nil, errors.New("invalid capability JSON fence")
		}
		text = strings.TrimSpace(strings.TrimSuffix(body, "```"))
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, errors.New("capability response is not a JSON object")
	}
	actual := make(map[string]json.RawMessage)
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, errors.New("invalid capability JSON key")
		}
		name, ok := key.(string)
		if !ok || actual[name] != nil {
			return nil, errors.New("invalid or repeated capability JSON key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, errors.New("incomplete capability JSON value")
		}
		var answerText string
		if json.Unmarshal(value, &answerText) == nil && codexQualityCapabilityRefused(answerText) {
			return nil, errors.New("capability response refused")
		}
		actual[name] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("incomplete capability JSON or trailing text")
	}
	return actual, nil
}

func codexQualityCapabilityRefused(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"i cannot", "i can't", "unable to", "cannot comply", "cannot assist", "cannot answer", "can't answer", "sorry", "无法", "不能", "抱歉"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
