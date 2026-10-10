package config

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"

	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

// Both raw inputs have passed their original strict loaders before material is
// compared. Keep the exact loaded Account input; another LookupEnv call could
// return a different ring. This projection is not an alternate key decoder.
func loadKnowledgeConfirmationKeys(raw, accountRaw string, cfg Config) (kc.ConfirmationKeys, error) {
	keys, err := kc.LoadConfirmationKeys(raw)
	if err != nil {
		return kc.ConfirmationKeys{}, invalid("KNOWLEDGE_CONFIRMATION_KEYRING")
	}
	type materialRing struct {
		Keys []struct {
			Key string `json:"key_b64"`
		} `json:"keys"`
	}
	var confirmation, account materialRing
	if json.Unmarshal([]byte(raw), &confirmation) != nil || json.Unmarshal([]byte(accountRaw), &account) != nil {
		return kc.ConfirmationKeys{}, invalid("KNOWLEDGE_CONFIRMATION_KEYRING")
	}
	for _, entry := range confirmation.Keys {
		material, err := base64.StdEncoding.Strict().DecodeString(entry.Key)
		if err != nil || len(material) != 32 {
			clear(material)
			return kc.ConfirmationKeys{}, invalid("KNOWLEDGE_CONFIRMATION_KEYRING")
		}
		reused := cfg.cursorKeys.ContainsMaterial(material) || cfg.secretKeys.ContainsMaterial(material) || cfg.objectRuntime.DownloadKeyring().ContainsMaterial(material)
		for _, prior := range account.Keys {
			other, decodeErr := base64.StdEncoding.Strict().DecodeString(prior.Key)
			if decodeErr != nil || len(other) != 32 || subtle.ConstantTimeCompare(material, other) == 1 {
				reused = true
			}
			clear(other)
		}
		clear(material)
		if reused {
			return kc.ConfirmationKeys{}, invalid("KNOWLEDGE_CONFIRMATION_KEYRING")
		}
	}
	return keys, nil
}
