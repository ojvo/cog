package json

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadJSONFile reads a JSON file and unmarshals it into dst.
func LoadJSONFile(filename string, dst any) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read file %s error: %w", filename, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("unmarshal %s error: %w", filename, err)
	}
	return nil
}

// SaveJSONFile marshals v as compact JSON and writes it to filename.
func SaveJSONFile(filename string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s error: %w", filename, err)
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return fmt.Errorf("write %s error: %w", filename, err)
	}
	return nil
}

// SaveJSONPrettyFile marshals v as pretty JSON and writes it to filename.
func SaveJSONPrettyFile(filename string, v any) error {
	data, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal %s error: %w", filename, err)
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return fmt.Errorf("write %s error: %w", filename, err)
	}
	return nil
}
