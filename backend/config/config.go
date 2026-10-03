package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func RequiredString(name string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("%s environment variable is empty", name)
	}
	return value, nil
}

func OptionalString(name, defaultValue string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue
	}
	return value
}

func Bool(name string, defaultValue bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("failed to parse %s environment variable: %v", name, err)
	}
	return parsed, nil
}

func PositiveInt(name string, defaultValue int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("failed to parse %s environment variable: %v", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s environment variable must be positive", name)
	}
	return parsed, nil
}

func BoundedInt(name string, defaultValue, minValue, maxValue int) (int, error) {
	parsed, err := PositiveInt(name, defaultValue)
	if err != nil {
		return 0, err
	}
	if parsed < minValue || parsed > maxValue {
		return 0, fmt.Errorf("%s environment variable must be between %d and %d", name, minValue, maxValue)
	}
	return parsed, nil
}

func StringList(name string, maxItems int) ([]string, error) {
	value, err := RequiredString(name)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		result = append(result, item)
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("%s environment variable does not contain any values", name)
	}
	if maxItems > 0 && len(result) > maxItems {
		return nil, fmt.Errorf("%s environment variable contains %d values, maximum is %d", name, len(result), maxItems)
	}

	return result, nil
}
