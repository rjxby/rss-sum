package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// LoadEnvFile fills unset environment variables from an optional .env file.
func LoadEnvFile(path string) error {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read environment file %q: %w", path, err)
	}

	values, err := godotenv.Unmarshal(string(content))
	if err != nil {
		// Parser errors can contain configuration values, including secrets.
		return fmt.Errorf("invalid syntax in environment file %q", path)
	}
	for name, value := range values {
		if _, exists := os.LookupEnv(name); exists {
			continue
		}
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("failed to set environment variable %q: %w", name, err)
		}
	}
	return nil
}
