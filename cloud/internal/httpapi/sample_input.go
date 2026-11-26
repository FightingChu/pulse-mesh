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
		Value *float64        `json:"value"`
		TempC json.RawMessage `json:"temp_c"`
		Ts    json.RawMessage `json:"ts"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.Value == nil {
		return errors.New("value is required and must be a number")
	}
	decoded.Sample.Value = *decoded.Value
	if len(decoded.TempC) > 0 {
		if string(decoded.TempC) == "null" {
			return errors.New("temp_c must be a number when provided")
		}
		if err := json.Unmarshal(decoded.TempC, &decoded.Sample.TempC); err != nil {
			return errors.New("temp_c must be a number when provided")
		}
	}
	if len(decoded.Ts) > 0 {
		if string(decoded.Ts) == "null" {
			return errors.New("ts must be a timestamp when provided")
		}
		if err := json.Unmarshal(decoded.Ts, &decoded.Sample.Ts); err != nil {
			return err
		}
	}
	input.Sample = decoded.Sample
	return nil
}
