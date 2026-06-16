package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"math/big"
)

// parseJWK converts a JWK key descriptor into a Go crypto public key.
// Supports RSA keys (kty=RSA) used by Entra ID and Google.
func parseJWK(jwk jwkKey) (any, error) {
	switch jwk.Kty {
	case "RSA":
		return parseRSAPublicKey(jwk)
	default:
		return nil, fmt.Errorf("unsupported kty: %s", jwk.Kty)
	}
}

// parseRSAPublicKey constructs an rsa.PublicKey from JWK parameters n and e.
func parseRSAPublicKey(jwk jwkKey) (*rsa.PublicKey, error) {
	if jwk.N == "" || jwk.E == "" {
		return nil, fmt.Errorf("missing RSA key parameters")
	}

	nBytes, err := base64.RawURLEncoding.DecodeString(jwk.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(jwk.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}
