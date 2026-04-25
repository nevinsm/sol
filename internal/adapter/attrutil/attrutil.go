// Package attrutil provides shared attribute parsing helpers for adapter packages.
package attrutil

import "strconv"

// ParseInt parses an integer attribute value, returning 0 on failure.
func ParseInt(attrs map[string]string, key string) int64 {
	v, ok := attrs[key]
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// ParseFloat parses a float attribute value, returning nil if absent or invalid.
func ParseFloat(attrs map[string]string, key string) *float64 {
	v, ok := attrs[key]
	if !ok {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}

// ParseIntPtr parses an integer attribute value, returning nil if absent or invalid.
func ParseIntPtr(attrs map[string]string, key string) *int64 {
	v, ok := attrs[key]
	if !ok {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}
