package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"

	"github.com/libp2p/go-libp2p/core/crypto"
)

const identityFileName = ".identity"

type storedIdentity struct {
	Version    int    `json:"version"`
	PrivateKey []byte `json:"private_key"`
}

func LoadOrCreateIdentity(path string) (crypto.PrivKey, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		return parseStoredIdentity(raw)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, err
	}

	record := storedIdentity{
		Version: 1,
	}

	record.PrivateKey, err = crypto.MarshalPrivateKey(priv)
	if err != nil {
		return nil, err
	}

	payload, err := json.MarshalIndent(record, "", " ")
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')

	if err = os.WriteFile(path, payload, 0o600); err != nil {
		return nil, err
	}
	return priv, nil

}
func parseStoredIdentity(raw []byte) (crypto.PrivKey, error) {
	var record storedIdentity

	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, fmt.Errorf("invalid %s file: %w", identityFileName, err)
	}
	if record.Version != 1 {
		return nil, fmt.Errorf(
			"unsupported %s version: %d",
			identityFileName,
			record.Version,
		)
	}

	if len(record.PrivateKey) == 0 {
		return nil, fmt.Errorf("%s private key is empty", identityFileName)
	}

	priv, err := crypto.UnmarshalPrivateKey(record.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid %s private key: %w",
			identityFileName,
			err,
		)
	}
	return priv, nil
}
