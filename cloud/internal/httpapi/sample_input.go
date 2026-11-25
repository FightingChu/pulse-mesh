package httpapi

import (
	"encoding/json"
	"errors"

	"pulse-mesh/cloud/internal/ingest"
)

type sampleInput struct {
	ingest.Sample
}

func (input *sampleInput) UnmarshalJSON(data []byte) error {
	var decoded struct {
		ingest.Sample
		Value *float64 `json:"value"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Value == nil {
		return errors.New("value is required and must be a number")
	}
	decoded.Sample.Value = *decoded.Value
	input.Sample = decoded.Sample
	return nil
}
