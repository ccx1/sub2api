package modelquality

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

type referenceFixture struct {
	Samples []struct {
		Sample Sample `json:"sample"`
	} `json:"samples"`
	Cases []struct {
		Indices     []int  `json:"sample_indices"`
		Candidate   string `json:"candidate"`
		UsedOutputs int    `json:"used_outputs"`
		Candidates  []struct {
			Model       string  `json:"model"`
			Probability float64 `json:"probability"`
			Similarity  float64 `json:"profile_similarity"`
			Score       float64 `json:"score"`
		} `json:"candidates"`
	} `json:"cases"`
}

func loadReference(t *testing.T) referenceFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture referenceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// Golden scores were computed by upstream fingerprint.py at 55a2e4a and
// cross-checked against static/fingerprint-core.js, including every candidate.
func TestAnalyzeMatchesUpstreamReference(t *testing.T) {
	fixture := loadReference(t)
	for _, test := range fixture.Cases {
		t.Run(fmt.Sprint(test.Indices), func(t *testing.T) {
			outputs := make([]Sample, len(test.Indices))
			for i, index := range test.Indices {
				outputs[i] = fixture.Samples[index].Sample
			}
			result, err := Analyze(outputs)
			if err != nil {
				t.Fatal(err)
			}
			if result.Candidate != test.Candidate || result.UsedOutputs != test.UsedOutputs {
				t.Fatalf("prediction = %s, used = %d", result.Candidate, result.UsedOutputs)
			}
			if len(result.Candidates) != len(test.Candidates) {
				t.Fatalf("candidate count = %d", len(result.Candidates))
			}
			for i, expected := range test.Candidates {
				actual := result.Candidates[i]
				if actual.Model != expected.Model {
					t.Fatalf("candidate %d = %s, want %s", i, actual.Model, expected.Model)
				}
				assertClose(t, actual.Probability, expected.Probability)
				assertClose(t, actual.Similarity, expected.Similarity)
				assertClose(t, actual.Score, expected.Score)
			}
			assertClose(t, result.Probability, result.Candidates[0].Probability)
			assertClose(t, result.Similarity, result.Candidates[0].Similarity)
			assertClose(t, result.Margin, result.Candidates[0].Probability-result.Candidates[1].Probability)
		})
	}
}

func assertClose(t *testing.T, actual, expected float64) {
	t.Helper()
	if !finite(actual) || math.Abs(actual-expected) > 1e-11 {
		t.Fatalf("got %.17g, want %.17g", actual, expected)
	}
}

func TestAnalyzeRejectsInvalidSamples(t *testing.T) {
	valid := loadReference(t).Samples[0].Sample
	tests := map[string]Sample{
		"empty":              {Text: "", ExpectedCount: 300},
		"no values":          {Text: "[]", ExpectedCount: 300},
		"null":               {Text: "null", ExpectedCount: 300},
		"too few":            {Text: "[1,2,3]", ExpectedCount: 300},
		"constant":           sequenceSample(300, func(int) int { return 1 }),
		"increasing":         sequenceSample(300, func(i int) int { return i + 1 }),
		"decreasing":         sequenceSample(300, func(i int) int { return 355 - i }),
		"repeated block":     sequenceSample(300, func(i int) int { return []int{2, 49, 7, 200, 39, 4}[i%6] }),
		"modular arithmetic": sequenceSample(300, func(i int) int { return (17+73*i)%355 + 1 }),
		"negative":           sequenceSample(300, func(int) int { return -1 }),
		"out of range":       sequenceSample(300, func(int) int { return 356 }),
		"explanation":        {Text: "I selected these numbers: " + valid.Text, ExpectedCount: valid.ExpectedCount},
		"truncated":          {Text: valid.Text[:len(valid.Text)-1], ExpectedCount: valid.ExpectedCount},
		"multiple arrays":    {Text: valid.Text + valid.Text, ExpectedCount: valid.ExpectedCount},
		"metadata object":    {Text: `{"model":"gpt-5.5","numbers":` + valid.Text + `}`, ExpectedCount: valid.ExpectedCount},
		"metadata suffix":    {Text: valid.Text + " model=gpt-5.5", ExpectedCount: valid.ExpectedCount},
		"fraction":           {Text: strings.Replace(valid.Text, "[", "[1.5,", 1), ExpectedCount: valid.ExpectedCount},
		"null item":          {Text: strings.Replace(valid.Text, "[", "[null,", 1), ExpectedCount: valid.ExpectedCount},
		"unspecified count":  {Text: valid.Text},
		"oversized count":    {Text: valid.Text, ExpectedCount: 1001},
		"oversized text":     {Text: strings.Repeat(" ", 33*1024) + valid.Text, ExpectedCount: valid.ExpectedCount},
	}
	for name, sample := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := Analyze([]Sample{sample})
			if err == nil || result.UsedOutputs != 0 || result.Candidate != "" {
				t.Fatalf("invalid sample produced a candidate: %+v, err = %v", result, err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Accepted || result.Diagnostics[0].Reason == "" {
				t.Fatalf("missing invalid-sample reason: %+v", result.Diagnostics)
			}
		})
	}
}

func sequenceSample(count int, value func(int) int) Sample {
	numbers := make([]int, count)
	for i := range numbers {
		numbers[i] = value(i)
	}
	text, _ := json.Marshal(numbers)
	return Sample{Text: string(text), ExpectedCount: count}
}

func TestInvalidSamplesDoNotInflateConfidence(t *testing.T) {
	valid := loadReference(t).Samples[0].Sample
	single, err := Analyze([]Sample{valid})
	if err != nil {
		t.Fatal(err)
	}
	mixed, err := Analyze([]Sample{valid, sequenceSample(300, func(int) int { return 1 })})
	if err != nil {
		t.Fatal(err)
	}
	if mixed.UsedOutputs != 1 || mixed.Diagnostics[1].Accepted {
		t.Fatalf("invalid sample contributed: %+v", mixed)
	}
	assertClose(t, mixed.Probability, single.Probability)
	assertClose(t, mixed.Similarity, single.Similarity)
}

func TestAnalyzeAcceptsSingleJSONFence(t *testing.T) {
	sample := loadReference(t).Samples[0].Sample
	for _, newline := range []string{"\n", "\r\n"} {
		fenced := Sample{Text: " \n```json" + newline + sample.Text + newline + "```\n ", ExpectedCount: sample.ExpectedCount}
		result, err := Analyze([]Sample{fenced})
		if err != nil || result.UsedOutputs != 1 {
			t.Fatalf("fenced sample rejected: %v", err)
		}
	}
}

func TestDuplicateSamplesDoNotInflateConfidence(t *testing.T) {
	sample := loadReference(t).Samples[0].Sample
	single, err := Analyze([]Sample{sample})
	if err != nil {
		t.Fatal(err)
	}
	fenced := Sample{Text: "```json\n" + sample.Text + "\n```", ExpectedCount: sample.ExpectedCount}
	duplicate, err := Analyze([]Sample{sample, fenced})
	if err != nil || duplicate.UsedOutputs != 1 || duplicate.Diagnostics[1].Accepted {
		t.Fatalf("duplicate provided independent evidence: %+v, err = %v", duplicate, err)
	}
	assertClose(t, duplicate.Probability, single.Probability)
}

func TestChallengeAndBankMetadata(t *testing.T) {
	for range 100 {
		prompt, count := GenerateFingerprintChallenge()
		if count < 292 || count > 332 || !strings.Contains(prompt, fmt.Sprint(count)) || !strings.Contains(prompt, "JSON array") {
			t.Fatalf("invalid challenge: %d, %s", count, prompt)
		}
	}
	if !SupportsModel("gpt-5.5") || !SupportsModel("claude-opus-4-6") {
		t.Fatal("known model not supported")
	}
	for _, model := range []string{"", "gpt-latest", "gpt-5.5-unknown", "GPT-5.5", "gpt-5.3"} {
		if SupportsModel(model) {
			t.Fatalf("unknown model %q unexpectedly supported", model)
		}
	}
	if BankVersion != "modeltrace-55a2e4a" {
		t.Fatalf("unexpected bank revision %q", BankVersion)
	}
}

func TestAnalyzeEnforcesCalibratedSampleCount(t *testing.T) {
	if _, err := Analyze(nil); err == nil {
		t.Fatal("empty samples accepted")
	}
	sample := loadReference(t).Samples[0].Sample
	if _, err := Analyze([]Sample{sample, sample, sample, sample}); err == nil {
		t.Fatal("uncalibrated query count accepted")
	}
}
