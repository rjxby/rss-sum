package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func unsetTestEnv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	require.NoError(t, os.Unsetenv(name))
}

func TestLoadEnvFile(t *testing.T) {
	unsetTestEnv(t, "RSS_SUM_TEST_FEEDS")
	unsetTestEnv(t, "RSS_SUM_TEST_KEY")
	t.Setenv("RSS_SUM_TEST_OVERRIDE", "exported")
	t.Setenv("RSS_SUM_TEST_EMPTY", "")

	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte(`# Local settings
RSS_SUM_TEST_FEEDS="https://go.dev/blog/feed.atom,https://example.com/rss"
export RSS_SUM_TEST_KEY='a key with spaces # and equals=characters'
RSS_SUM_TEST_OVERRIDE=from-file
RSS_SUM_TEST_EMPTY=from-file
`), 0o600))

	require.NoError(t, LoadEnvFile(path))
	require.Equal(t, "https://go.dev/blog/feed.atom,https://example.com/rss", os.Getenv("RSS_SUM_TEST_FEEDS"))
	require.Equal(t, "a key with spaces # and equals=characters", os.Getenv("RSS_SUM_TEST_KEY"))
	require.Equal(t, "exported", os.Getenv("RSS_SUM_TEST_OVERRIDE"))
	value, exists := os.LookupEnv("RSS_SUM_TEST_EMPTY")
	require.True(t, exists)
	require.Empty(t, value)
}

func TestLoadEnvFileMissing(t *testing.T) {
	require.NoError(t, LoadEnvFile(filepath.Join(t.TempDir(), ".env")))
}

func TestLoadEnvFileUnreadable(t *testing.T) {
	require.ErrorContains(t, LoadEnvFile(t.TempDir()), "failed to read environment file")
}

func TestLoadEnvFileInvalid(t *testing.T) {
	unsetTestEnv(t, "RSS_SUM_TEST_VALID")
	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte("RSS_SUM_TEST_VALID=value\nRSS_SUM_TEST_BROKEN='secret-value\n"), 0o600))

	err := LoadEnvFile(path)
	require.ErrorContains(t, err, "invalid syntax")
	require.NotContains(t, err.Error(), "secret-value")
	_, exists := os.LookupEnv("RSS_SUM_TEST_VALID")
	require.False(t, exists)
}
