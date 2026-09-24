package service

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexQualityCapabilityAnswerRules(t *testing.T) {
	in := codexQualityCapabilityInputs{
		candidates: []int{4, 10, 10, 12, 15, 20, 21}, minimum: 10, excludeDivisor: 3,
		transform: []int{-3, 0, 2, 7, -1}, offset: 4,
		digits: []int{5, 1, 9, 2, 3}, modulo: 13,
		conditions: [][3]bool{{true, false, false}, {true, true, false}, {false, true, true}, {false, false, false}},
		start:      7, multiplier: 3, addend: 9, divisor: 5,
	}
	require.Equal(t, codexQualityCapabilityAnswer{
		Selected: []int{10, 10, 20}, Transformed: []int{-7, 17, 2, -4, -13},
		Checksum: 5, Flags: []bool{true, false, true, false}, Final: 6,
	}, codexQualityCapabilityExpected(in))
	in.addend = 10
	in.minimum = 31
	answer := codexQualityCapabilityExpected(in)
	require.Equal(t, 29, answer.Final, "the non-divisible branch subtracts two")
	require.Equal(t, []int{}, answer.Selected, "an empty answer must encode as [] instead of null")
}

func codexQualityCapabilityAnswerJSON(t *testing.T, challenge codexQualityChallenge) string {
	t.Helper()
	encoded, err := json.Marshal(challenge.expected)
	require.NoError(t, err)
	return string(encoded)
}

func TestCodexQualityCapabilityScoresCompleteStructuredAnswers(t *testing.T) {
	challenge := codexQualityCapabilityChallengeWithRand(rand.New(rand.NewPCG(4, 7)))
	correct := codexQualityCapabilityAnswerJSON(t, challenge)
	for _, response := range []string{correct, " \n" + correct + "\n ", "```json\n" + correct + "\n```", "```\n" + correct + "\n```", "```JSON\r\n" + correct + "\r\n```"} {
		score, err := scoreCodexQualityCapability(challenge, response)
		require.NoError(t, err)
		require.Equal(t, float64(100), score)
	}
	var actual map[string]any
	require.NoError(t, json.Unmarshal([]byte(correct), &actual))
	actual["checksum"] = challenge.expected.Checksum + 1
	assertCodexQualityCapabilityScore(t, challenge, actual, 80)
	actual["transformed"] = []int{}
	assertCodexQualityCapabilityScore(t, challenge, actual, 60)
	actual["selected"], actual["flags"], actual["final"] = []int{-999}, []bool{}, -999
	assertCodexQualityCapabilityScore(t, challenge, actual, 0)
}

func assertCodexQualityCapabilityScore(t *testing.T, challenge codexQualityChallenge, actual map[string]any, expected float64) {
	t.Helper()
	encoded, err := json.Marshal(actual)
	require.NoError(t, err)
	score, err := scoreCodexQualityCapability(challenge, string(encoded))
	require.NoError(t, err)
	require.Equal(t, expected, score)
}

func TestCodexQualityCapabilityDoesNotTreatInvalidResponsesAsWrongAnswers(t *testing.T) {
	challenge := newCodexQualityCapabilityChallenge()
	correct := codexQualityCapabilityAnswerJSON(t, challenge)
	responses := []string{
		"", " \n\t", "I cannot complete this task", `{"error":"refused"}`, `{}`,
		"[]", "null", "42", `"answer"`, correct[:len(correct)-1],
		correct + " explanation", correct + " {}", correct + " null",
		"```python\n" + correct + "\n```", "```json\n" + correct,
		"```json\n" + correct + "\n```\nextra", "````json\n" + correct + "\n````",
		`{"final":1,"final":2}`, `{"final":1,}`, `{"final":"I cannot answer"}`,
		`{"final":"\u62b1\u6b49"}`, strings.Repeat(" ", 1) + strings.Repeat("x", 16<<10+1),
	}
	for _, response := range responses {
		score, err := scoreCodexQualityCapability(challenge, response)
		require.Error(t, err, "invalid response: %.100s", response)
		require.Zero(t, score)
	}
}

func codexQualityInvalidSchemaResponses(t *testing.T) map[string]string {
	t.Helper()
	responses := map[string]string{
		"missing answer": `{"selected":[],"transformed":[],"checksum":0,"flags":[]}`,
		"unknown answer": `{"selected":[],"transformed":[],"checksum":0,"flags":[],"final":0,"extra":0}`,
	}
	for key, invalidValues := range map[string][]string{
		"selected":    {`null`, `[null]`, `[1,null]`, `["1"]`, `[1.5]`, `{}`},
		"transformed": {`null`, `[null]`, `[false]`, `"incorrect type"`},
		"checksum":    {`null`, `"0"`, `0.5`, `true`, `[]`},
		"flags":       {`null`, `[null]`, `[false,null]`, `[0]`, `["true"]`},
		"final":       {`null`, `"0"`, `0.5`, `false`, `{}`},
	} {
		for _, value := range invalidValues {
			actual := map[string]json.RawMessage{
				"selected": []byte(`[]`), "transformed": []byte(`[]`), "checksum": []byte(`0`),
				"flags": []byte(`[]`), "final": []byte(`0`),
			}
			actual[key] = json.RawMessage(value)
			encoded, err := json.Marshal(actual)
			require.NoError(t, err)
			responses[key+"="+value] = string(encoded)
		}
	}
	return responses
}

func TestCodexQualityCapabilityRejectsNullAndWrongTypes(t *testing.T) {
	challenge := newCodexQualityCapabilityChallenge()
	for name, response := range codexQualityInvalidSchemaResponses(t) {
		t.Run(name, func(t *testing.T) {
			score, err := scoreCodexQualityCapability(challenge, response)
			require.Error(t, err)
			require.Zero(t, score)
		})
	}
}

func TestCodexQualityCapabilityRandomCasesStayBoundedAndScoreable(t *testing.T) {
	prompts := make(map[string]struct{})
	for seed := uint64(0); seed < 100; seed++ {
		challenge := codexQualityCapabilityChallengeWithRand(rand.New(rand.NewPCG(seed, seed^12345)))
		require.Equal(t, "capability", challenge.Kind)
		require.Equal(t, 5, challenge.ExpectedCount)
		require.Less(t, len(challenge.Prompt), 1500)
		answer := codexQualityCapabilityAnswerJSON(t, challenge)
		require.Less(t, len(answer), 300)
		score, err := scoreCodexQualityCapability(challenge, answer)
		require.NoError(t, err)
		require.Equal(t, float64(100), score)
		prompts[challenge.Prompt] = struct{}{}
	}
	require.Len(t, prompts, 100, "independent probes must not reuse a static question")
	_, err := scoreCodexQualityCapability(codexQualityChallenge{}, `{}`)
	require.Error(t, err)
}
