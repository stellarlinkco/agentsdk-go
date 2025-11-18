package plugins

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// TrustStore keeps signer public keys and enforces signature + digest policies.
type TrustStore struct {
	mu             sync.RWMutex
	keys           map[string]ed25519.PublicKey
	blockedDigests map[string]struct{}
	allowUnsigned  bool
}

// NewTrustStore builds an empty trust store.
func NewTrustStore() *TrustStore {
	return &TrustStore{
		keys:           make(map[string]ed25519.PublicKey),
		blockedDigests: make(map[string]struct{}),
	}
}

// Register adds a signer to the trust store.
func (t *TrustStore) Register(id string, public ed25519.PublicKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.keys[id] = public
}

// BlockDigest permanently revokes a plugin digest.
func (t *TrustStore) BlockDigest(digest string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.blockedDigests[strings.ToLower(digest)] = struct{}{}
}

// AllowUnsigned configures whether manifests without signatures pass validation.
func (t *TrustStore) AllowUnsigned(allow bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.allowUnsigned = allow
}

func (t *TrustStore) isDigestBlocked(digest string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, blocked := t.blockedDigests[strings.ToLower(digest)]
	return blocked
}

// Verify enforces signature rules for a manifest.
func (t *TrustStore) Verify(mf *Manifest, payload []byte) error {
	if t == nil {
		return errors.New("trust store is nil")
	}
	if mf == nil {
		return errors.New("manifest is nil")
	}
	if t.isDigestBlocked(mf.Digest) {
		return fmt.Errorf("plugin digest %s is blocked", mf.Digest)
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if mf.Signature == "" || mf.Signer == "" {
		if t.allowUnsigned {
			return nil
		}
		return errors.New("unsigned plugins are rejected")
	}
	key, ok := t.keys[mf.Signer]
	if !ok {
		return fmt.Errorf("unknown signer %s", mf.Signer)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(mf.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	hashed := sha256.Sum256(payload)
	if !ed25519.Verify(key, hashed[:], sigBytes) {
		return errors.New("signature verification failed")
	}
	return nil
}

// CanonicalManifestBytes serializes a manifest deterministically for signing.
func CanonicalManifestBytes(mf *Manifest) ([]byte, error) {
	if mf == nil {
		return nil, errors.New("manifest is nil")
	}
	caps := append([]string(nil), mf.Capabilities...)
	sort.Strings(caps)
	meta := make(map[string]string, len(mf.Metadata))
	var keys []string
	for k, v := range mf.Metadata {
		meta[k] = v
		keys = append(keys, k)
	}
	sort.Strings(keys)
	orderedMeta := make([][2]string, 0, len(keys))
	for _, k := range keys {
		orderedMeta = append(orderedMeta, [2]string{k, meta[k]})
	}
	payload := struct {
		Name         string      `json:"name"`
		Version      string      `json:"version"`
		Entrypoint   string      `json:"entrypoint"`
		Capabilities []string    `json:"capabilities"`
		Metadata     [][2]string `json:"metadata"`
		Digest       string      `json:"digest"`
		Signer       string      `json:"signer"`
	}{
		Name:         mf.Name,
		Version:      mf.Version,
		Entrypoint:   mf.Entrypoint,
		Capabilities: caps,
		Metadata:     orderedMeta,
		Digest:       strings.ToLower(mf.Digest),
		Signer:       mf.Signer,
	}
	return json.Marshal(payload)
}

// SignManifest signs a manifest payload with a private key for tests/tooling.
func SignManifest(mf *Manifest, private ed25519.PrivateKey) (string, error) {
	payload, err := CanonicalManifestBytes(mf)
	if err != nil {
		return "", err
	}
	hashed := sha256.Sum256(payload)
	signature := ed25519.Sign(private, hashed[:])
	return base64.StdEncoding.EncodeToString(signature), nil
}
