package httpapi

import (
	"encoding/json"
	"fmt"

	"pulse-mesh/cloud/internal/diagnose"
)

type diagnosisInput struct {
	diagnose.Request
}

func (input *diagnosisInput) UnmarshalJSON(data []byte) error {
	var decoded struct {
		diagnose.Request
		Values     []*float64 `json:"values"`
		RotationHz *float64   `json:"rotation_hz"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	values := make([]float64, len(decoded.Values))
	for index, value := range decoded.Values {
		if value == nil {
			return fmt.Errorf("values[%d] must be a number, not null", index)
		}
		values[index] = *value
	}
	decoded.Request.Values = values
	decoded.Request.RotationHz = 25
	if decoded.RotationHz != nil {
		decoded.Request.RotationHz = *decoded.RotationHz
	}
	input.Request = decoded.Request
	return nil
}
