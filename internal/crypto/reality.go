package crypto

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
)

// X25519Pair generates Reality-compatible private/public keys (URL-safe base64, no padding).
func X25519Pair() (privateKey, publicKey string, err error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	pub := priv.PublicKey()
	privateKey = base64.RawURLEncoding.EncodeToString(priv.Bytes())
	publicKey = base64.RawURLEncoding.EncodeToString(pub.Bytes())
	return privateKey, publicKey, nil
}

// PublicFromPrivate derives Reality public key from a private key string.
func PublicFromPrivate(privateKey string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(privateKey)
	if err != nil {
		// try standard base64
		raw, err = base64.StdEncoding.DecodeString(privateKey)
		if err != nil {
			return "", err
		}
	}
	curve := ecdh.X25519()
	priv, err := curve.NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()), nil
}

// RandomShortID returns hex shortId for Reality (default 8 hex chars = 4 bytes).
func RandomShortID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 8)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out), nil
}
