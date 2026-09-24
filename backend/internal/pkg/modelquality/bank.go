package modelquality

import (
	_ "embed"
	"encoding/json"
	"errors"
	"math"
	"sync"
)

//go:embed data/unified_bank.json
var bankJSON []byte

type featureArtifact struct {
	Mean         []float64     `json:"feature_mean"`
	Scale        []float64     `json:"feature_scale"`
	Basis        [][]float64   `json:"nuisance_basis"`
	Centroids    [][]float64   `json:"centroids"`
	Environments [][][]float64 `json:"environment_centroids"`
	Weight       float64       `json:"weight"`
}

type referenceBank struct {
	Models []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		Counts      []int  `json:"counts"`
	} `json:"models"`
	Robust struct {
		ModelOrder []string        `json:"model_order"`
		Hellinger  featureArtifact `json:"hellinger"`
		Ordered    featureArtifact `json:"ordered_blocks"`
	} `json:"robust"`
	Calibration map[string]struct {
		Beta float64 `json:"beta"`
	} `json:"calibration"`
}

var loadBank = sync.OnceValues(func() (*referenceBank, error) {
	var bank referenceBank
	if err := json.Unmarshal(bankJSON, &bank); err != nil {
		return nil, errors.New("invalid embedded fingerprint bank JSON")
	}
	if !bank.valid() {
		return nil, errors.New("invalid embedded fingerprint bank dimensions")
	}
	return &bank, nil
})

// SupportsModel intentionally uses exact IDs: unknown models and aliases must
// not inherit another model's reference distribution without calibration.
func SupportsModel(modelID string) bool {
	bank, err := loadBank()
	if err != nil {
		return false
	}
	for _, model := range bank.Models {
		if model.ID == modelID {
			return true
		}
	}
	return false
}

func (bank *referenceBank) valid() bool {
	count := len(bank.Models)
	if count < 2 || count != len(bank.Robust.ModelOrder) {
		return false
	}
	for i, model := range bank.Models {
		if model.ID == "" || model.ID != bank.Robust.ModelOrder[i] || len(model.Counts) != dimension {
			return false
		}
		for _, count := range model.Counts {
			if count < 0 {
				return false
			}
		}
	}
	for _, key := range []string{"1", "2", "3"} {
		if !finite(bank.Calibration[key].Beta) || bank.Calibration[key].Beta <= 0 {
			return false
		}
	}
	return bank.Robust.Hellinger.valid(dimension, count) && bank.Robust.Ordered.valid(74, count)
}

func (artifact *featureArtifact) valid(size, modelCount int) bool {
	if !validVector(artifact.Mean, size) || !validVector(artifact.Scale, size) || len(artifact.Centroids) != modelCount {
		return false
	}
	for _, scale := range artifact.Scale {
		if scale <= 0 {
			return false
		}
	}
	if !validMatrix(artifact.Basis, size) || !validMatrix(artifact.Centroids, size) {
		return false
	}
	if !finite(artifact.Weight) || artifact.Weight < 0 || artifact.Weight > 1 {
		return false
	}
	if artifact.Weight > 0 && len(artifact.Environments) == 0 {
		return false
	}
	for _, environment := range artifact.Environments {
		if len(environment) != modelCount || !validMatrix(environment, size) {
			return false
		}
	}
	return true
}

func validMatrix(matrix [][]float64, size int) bool {
	for _, vector := range matrix {
		if !validVector(vector, size) {
			return false
		}
	}
	return true
}

func validVector(vector []float64, size int) bool {
	if len(vector) != size {
		return false
	}
	for _, value := range vector {
		if !finite(value) {
			return false
		}
	}
	return true
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
