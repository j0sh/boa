package boa

import "fmt"

// ParseConfigBytes constructs a fresh defaulted value, decodes data using
// unmarshal, then validates it. The decoder owns the format and decoding policy.
// Explicit decoded zero, false, empty, and nil values replace defaults. No
// environment lookup, filesystem access or lifecycle hooks run automatically.
// On error it returns the value constructed so far and the original error.
// Use Defaults and Validate separately for other loading or validation policies.
func ParseConfigBytes[T any](data []byte, unmarshal func([]byte, any) error) (T, error) {
	value, err := Defaults[T]()
	if err != nil {
		return value, err
	}
	if unmarshal == nil {
		return value, fmt.Errorf("boa.ParseConfigBytes: decoder is required")
	}
	if err := unmarshal(data, &value); err != nil {
		return value, err
	}
	return value, Validate(value)
}
