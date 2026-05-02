package connectors

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/argon2"
)

const (
	vaultFile   = "vault.enc"
	argonTime   = 1
	argonMemory = 64 * 1024
	argonThreads = 4
	argonKeyLen = 32
	argonSalt   = "nexus-vault-salt-v1" // fixed salt; key strength comes from NEXUS_VAULT_KEY
)

// Vault stores encrypted credentials keyed by connector name and field name.
type Vault struct {
	key  []byte
	data map[string]map[string]string // connector → key → plaintext value
	path string
}

// NewVault loads or creates the vault at the given path using the supplied master key.
func NewVault(masterKey, path string) (*Vault, error) {
	key := deriveKey(masterKey)
	v := &Vault{key: key, path: path, data: make(map[string]map[string]string)}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := v.flush(); err != nil {
			return nil, fmt.Errorf("vault: create %s: %w", path, err)
		}
		return v, nil
	}

	if err := v.load(); err != nil {
		return nil, fmt.Errorf("vault: load: %w", err)
	}
	return v, nil
}

// Get returns the plaintext credential for a connector + key pair.
func (v *Vault) Get(connector, key string) (string, bool) {
	creds, ok := v.data[connector]
	if !ok {
		return "", false
	}
	val, ok := creds[key]
	return val, ok
}

// Set stores a credential and persists the vault.
func (v *Vault) Set(connector, key, value string) error {
	if v.data[connector] == nil {
		v.data[connector] = make(map[string]string)
	}
	v.data[connector][key] = value
	return v.flush()
}

func (v *Vault) load() error {
	ciphertext, err := os.ReadFile(v.path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	plaintext, err := decrypt(v.key, ciphertext)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	return json.Unmarshal(plaintext, &v.data)
}

func (v *Vault) flush() error {
	plaintext, err := json.Marshal(v.data)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	ciphertext, err := encrypt(v.key, plaintext)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	return os.WriteFile(v.path, ciphertext, 0600)
}

func deriveKey(masterKey string) []byte {
	return argon2.IDKey([]byte(masterKey), []byte(argonSalt), argonTime, argonMemory, argonThreads, argonKeyLen)
}

func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, data := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, data, nil)
}
