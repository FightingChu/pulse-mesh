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
		Values []*float64 `json:"values"`
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
	input.Request = decoded.Request
	return nil
}
