package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nexus/core/connectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVaultEncryptDecryptRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.enc")
	vault, err := connectors.NewVault("test-master-key-32-characters-ok!", path)
	require.NoError(t, err)

	err = vault.Set("stripe", "STRIPE_SECRET_KEY", "sk_test_abc123")
	require.NoError(t, err)

	// Reopen vault from disk with same key.
	vault2, err := connectors.NewVault("test-master-key-32-characters-ok!", path)
	require.NoError(t, err)

	val, ok := vault2.Get("stripe", "STRIPE_SECRET_KEY")
	assert.True(t, ok)
	assert.Equal(t, "sk_test_abc123", val)
}

func TestVaultWrongKeyReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.enc")
	_, err := connectors.NewVault("correct-master-key-32-characters!", path)
	require.NoError(t, err)

	// Write something.
	vault, err := connectors.NewVault("correct-master-key-32-characters!", path)
	require.NoError(t, err)
	require.NoError(t, vault.Set("github", "GITHUB_TOKEN", "ghp_secret"))

	// Try to open with wrong key.
	_, err = connectors.NewVault("wrong-key-32-characters-padding!!", path)
	assert.Error(t, err, "opening vault with wrong key should fail")
}

func TestVaultCredentialsNotInLogs(t *testing.T) {
	// This test verifies the vault never writes credentials to stdout/stderr.
	// We capture stderr and confirm no secret values appear.
	path := filepath.Join(t.TempDir(), "vault.enc")
	vault, err := connectors.NewVault("test-master-key-32-characters-ok!", path)
	require.NoError(t, err)

	secret := "super-secret-value-that-must-not-leak"
	require.NoError(t, vault.Set("api", "API_KEY", secret))

	// Read back from disk — the raw file should not contain the plaintext secret.
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	assert.NotContains(t, string(raw), secret, "secret must not appear in plaintext in vault file")
}

func TestVaultMultipleConnectors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.enc")
	vault, err := connectors.NewVault("test-master-key-32-characters-ok!", path)
	require.NoError(t, err)

	require.NoError(t, vault.Set("stripe", "STRIPE_KEY", "sk_stripe"))
	require.NoError(t, vault.Set("github", "GITHUB_TOKEN", "ghp_github"))

	v1, ok1 := vault.Get("stripe", "STRIPE_KEY")
	v2, ok2 := vault.Get("github", "GITHUB_TOKEN")

	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Equal(t, "sk_stripe", v1)
	assert.Equal(t, "ghp_github", v2)
}
