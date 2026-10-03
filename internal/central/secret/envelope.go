package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type payloadMarker struct{}
type payloadID = foundation.ID[payloadMarker]
type ownerKind byte

const (
	valueOwner   ownerKind = 1
	receiptOwner ownerKind = 2
)
const envelopeFormat = 1
const envelopeAlgorithm = "AES-256-GCM"
const maxMasterCounter = uint64(1<<32 - 1)

type envelope struct {
	id                                           payloadID
	scope                                        identity.Scope
	ownerKind                                    ownerKind
	ownerID                                      string
	format                                       int
	algorithm                                    string
	dataNonce, ciphertext, wrapNonce, wrappedDEK []byte
	masterVersion, wrapRevision                  foundation.Version
}

func aead(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func uuidBytes(raw string) ([]byte, error) {
	if _, err := foundation.ParseID[struct{}](raw); err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.ReplaceAll(raw, "-", ""))
}
func baseAAD(domain string, p envelope) ([]byte, error) {
	if p.format != envelopeFormat || p.algorithm != envelopeAlgorithm || p.id.Validate() != nil || p.scope.Validate() != nil || p.ownerKind != valueOwner && p.ownerKind != receiptOwner {
		return nil, errors.New("INVALID_ENVELOPE")
	}
	owner, err := uuidBytes(p.ownerID)
	if err != nil {
		return nil, err
	}
	id, _ := uuidBytes(p.id.String())
	scope := p.scope.Details()
	project := make([]byte, 16)
	var kind byte
	switch scope.Kind {
	case identity.System:
		kind = 1
	case identity.ProjectScope:
		kind = 2
		project, err = uuidBytes(scope.ProjectID)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("INVALID_ENVELOPE")
	}
	aad := append([]byte(domain), 0)
	aad = binary.BigEndian.AppendUint32(aad, envelopeFormat)
	aad = append(aad, kind)
	aad = append(aad, project...)
	aad = append(aad, byte(p.ownerKind))
	aad = append(aad, owner...)
	aad = append(aad, id...)
	return aad, nil
}
func dataAAD(p envelope) ([]byte, error) { return baseAAD("agenteam.secret.data.v1", p) }
func wrapAAD(p envelope) ([]byte, error) {
	aad, err := baseAAD("agenteam.secret.wrap.v1", p)
	if err != nil || p.masterVersion.Validate() != nil || len(p.dataNonce) != 12 || len(p.ciphertext) < 17 || len(p.ciphertext) > sc.MaxValueBytes+16 {
		return nil, errors.New("INVALID_ENVELOPE")
	}
	aad = binary.BigEndian.AppendUint64(aad, uint64(p.masterVersion))
	aad = append(aad, p.dataNonce...)
	digest := sha256.Sum256(p.ciphertext)
	aad = append(aad, digest[:]...)
	return aad, nil
}
func masterNonce(counter uint64) ([]byte, error) {
	if counter < 1 || counter > maxMasterCounter {
		return nil, failure(NonceExhausted, foundation.DependencyUnavailable, nil)
	}
	return binary.BigEndian.AppendUint64([]byte("ATK1"), counter), nil
}
func validMasterNonce(nonce []byte) bool {
	if len(nonce) != 12 || string(nonce[:4]) != "ATK1" {
		return false
	}
	counter := binary.BigEndian.Uint64(nonce[4:])
	return counter >= 1 && counter <= maxMasterCounter
}
func seal(keys Keyring, version foundation.Version, nonce []byte, scope identity.Scope, kind ownerKind, owner string, id payloadID, value []byte) (envelope, error) {
	p := envelope{id: id, scope: scope, ownerKind: kind, ownerID: owner, format: envelopeFormat, algorithm: envelopeAlgorithm, masterVersion: version, wrapRevision: 1}
	if len(value) < 1 || len(value) > sc.MaxValueBytes || kind == receiptOwner && len(value) != 32 || !validMasterNonce(nonce) {
		return envelope{}, invalid()
	}
	key, ok := keys.key(version)
	if !ok {
		return envelope{}, failure(KeyUnavailable, foundation.DependencyUnavailable, nil)
	}
	defer clear(key[:])
	dek := make([]byte, 32)
	defer clear(dek)
	p.dataNonce = make([]byte, 12)
	if _, err := rand.Read(dek); err != nil {
		return envelope{}, unavailable(err)
	}
	if _, err := rand.Read(p.dataNonce); err != nil {
		return envelope{}, unavailable(err)
	}
	aad, err := dataAAD(p)
	if err != nil {
		return envelope{}, invalid()
	}
	dataCipher, err := aead(dek)
	if err != nil {
		return envelope{}, unavailable(err)
	}
	p.ciphertext = dataCipher.Seal(nil, p.dataNonce, value, aad)
	aad, err = wrapAAD(p)
	if err != nil {
		return envelope{}, invalid()
	}
	masterCipher, err := aead(key[:])
	if err != nil {
		return envelope{}, unavailable(err)
	}
	p.wrapNonce = append([]byte(nil), nonce...)
	p.wrappedDEK = masterCipher.Seal(nil, p.wrapNonce, dek, aad)
	return p, nil
}
func unwrap(keys Keyring, p envelope) ([]byte, error) {
	bad := func() ([]byte, error) { return nil, failure(DecryptFailed, foundation.DependencyUnavailable, nil) }
	aad, err := wrapAAD(p)
	if err != nil || p.wrapRevision.Validate() != nil || !validMasterNonce(p.wrapNonce) || len(p.wrappedDEK) != 48 {
		return bad()
	}
	key, ok := keys.key(p.masterVersion)
	if !ok {
		return nil, failure(KeyUnavailable, foundation.DependencyUnavailable, nil)
	}
	defer clear(key[:])
	masterCipher, err := aead(key[:])
	if err != nil {
		return bad()
	}
	dek, err := masterCipher.Open(nil, p.wrapNonce, p.wrappedDEK, aad)
	if err != nil || len(dek) != 32 {
		clear(dek)
		return bad()
	}
	return dek, nil
}
func openEnvelope(keys Keyring, p envelope) ([]byte, error) {
	dek, err := unwrap(keys, p)
	if err != nil {
		return nil, err
	}
	defer clear(dek)
	aad, err := dataAAD(p)
	if err != nil {
		return nil, failure(DecryptFailed, foundation.DependencyUnavailable, nil)
	}
	dataCipher, err := aead(dek)
	if err != nil {
		return nil, unavailable(err)
	}
	value, err := dataCipher.Open(nil, p.dataNonce, p.ciphertext, aad)
	if err != nil || len(value) < 1 || len(value) > sc.MaxValueBytes || p.ownerKind == receiptOwner && len(value) != 32 {
		clear(value)
		return nil, failure(DecryptFailed, foundation.DependencyUnavailable, nil)
	}
	return value, nil
}
func rewrap(keys Keyring, p envelope, target foundation.Version, nonce []byte) (envelope, error) {
	if !validMasterNonce(nonce) {
		return envelope{}, invalid()
	}
	dek, err := unwrap(keys, p)
	if err != nil {
		return envelope{}, err
	}
	defer clear(dek)
	key, ok := keys.key(target)
	if !ok {
		return envelope{}, failure(KeyUnavailable, foundation.DependencyUnavailable, nil)
	}
	defer clear(key[:])
	p.masterVersion = target
	p.wrapRevision++
	if p.wrapRevision.Validate() != nil {
		return envelope{}, invalid()
	}
	p.wrapNonce = append([]byte(nil), nonce...)
	aad, err := wrapAAD(p)
	if err != nil {
		return envelope{}, invalid()
	}
	masterCipher, err := aead(key[:])
	if err != nil {
		return envelope{}, unavailable(err)
	}
	p.wrappedDEK = masterCipher.Seal(nil, p.wrapNonce, dek, aad)
	return p, nil
}

const canaryPlaintext = "agenteam.secret.canary.plaintext.v1"

func canaryAAD(version foundation.Version, fingerprint [32]byte) []byte {
	aad := append([]byte("agenteam.secret.canary.v1"), 0)
	aad = binary.BigEndian.AppendUint64(aad, uint64(version))
	return append(aad, fingerprint[:]...)
}
func sealCanary(keys Keyring, version foundation.Version, nonce []byte) ([]byte, error) {
	if !validMasterNonce(nonce) {
		return nil, invalid()
	}
	key, ok := keys.key(version)
	if !ok {
		return nil, failure(KeyUnavailable, foundation.DependencyUnavailable, nil)
	}
	defer clear(key[:])
	c, err := aead(key[:])
	if err != nil {
		return nil, unavailable(err)
	}
	return c.Seal(nil, nonce, []byte(canaryPlaintext), canaryAAD(version, keyFingerprint(key))), nil
}
func verifyCanary(keys Keyring, version foundation.Version, nonce, encrypted []byte) error {
	key, ok := keys.key(version)
	if !ok {
		return failure(KeyUnavailable, foundation.DependencyUnavailable, nil)
	}
	defer clear(key[:])
	if !validMasterNonce(nonce) {
		return failure(DecryptFailed, foundation.DependencyUnavailable, nil)
	}
	c, err := aead(key[:])
	if err != nil {
		return unavailable(err)
	}
	plain, err := c.Open(nil, nonce, encrypted, canaryAAD(version, keyFingerprint(key)))
	defer clear(plain)
	if err != nil || subtle.ConstantTimeCompare(plain, []byte(canaryPlaintext)) != 1 {
		return failure(DecryptFailed, foundation.DependencyUnavailable, nil)
	}
	return nil
}
