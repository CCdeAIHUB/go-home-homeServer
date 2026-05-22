package security

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tjfoc/gmsm/sm2"
	"github.com/tjfoc/gmsm/sm3"
	"github.com/tjfoc/gmsm/x509"
)

type Identity struct {
	Private   *sm2.PrivateKey
	PublicPEM string
}

func LoadOrCreateIdentity(path string) (*Identity, error) {
	if path == "" {
		return nil, errors.New("identity path is required")
	}
	if pem, err := os.ReadFile(path); err == nil {
		return ParseIdentity(pem)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	key, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	privatePEM, err := x509.WritePrivateKeyToPem(key, nil)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, privatePEM, 0o600); err != nil {
		return nil, err
	}
	return identityFromPrivate(key)
}

func ParseIdentity(privatePEM []byte) (*Identity, error) {
	key, err := x509.ReadPrivateKeyFromPem(privatePEM, nil)
	if err != nil {
		return nil, err
	}
	return identityFromPrivate(key)
}

func EncryptForPublicKey(publicPEM string, plaintext []byte) ([]byte, error) {
	publicKey, err := x509.ReadPublicKeyFromPem([]byte(publicPEM))
	if err != nil {
		return nil, err
	}
	return sm2.EncryptAsn1(publicKey, plaintext, rand.Reader)
}

func (i *Identity) Decrypt(ciphertext []byte) ([]byte, error) {
	if i == nil || i.Private == nil {
		return nil, errors.New("identity private key is not available")
	}
	return sm2.DecryptAsn1(i.Private, ciphertext)
}

func (i *Identity) DeviceID(prefix string) string {
	digest := sm3.New()
	_, _ = digest.Write([]byte(i.PublicPEM))
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(digest.Sum(nil)[:16]))
}

func identityFromPrivate(key *sm2.PrivateKey) (*Identity, error) {
	publicPEM, err := x509.WritePublicKeyToPem(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	return &Identity{Private: key, PublicPEM: string(publicPEM)}, nil
}
