package accountmail

import (
	"encoding/base64"
	"encoding/json"

	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// emailCodeMessage deliberately extracts only the six digits. The random
// Secret envelope salt is never mailed or included in a recovery-log record.
func emailCodeMessage(material sc.SecretMaterial) (sc.SecretMaterial, error) {
	var out sc.SecretMaterial
	e := material.Use(func(b []byte) error {
		var value struct{ Code, Salt, Purpose string }
		if json.Unmarshal(b, &value) != nil || len(value.Code) != 6 {
			return invalid()
		}
		for _, v := range value.Code {
			if v < '0' || v > '9' {
				return invalid()
			}
		}
		salt, e := base64.RawURLEncoding.DecodeString(value.Salt)
		if e != nil || len(salt) != 32 {
			return invalid()
		}
		clear(salt)
		var subject string
		switch value.Purpose {
		case "passkey_recovery":
			subject = "Passkey recovery"
		case "email_change":
			subject = "Email address change"
		case "initialization":
			subject = "Email verification"
		case "operator_recovery":
			subject = "Account recovery"
		default:
			return invalid()
		}
		out, e = sc.NewSecretMaterial([]byte("Agenteam " + subject + " verification code: " + value.Code + "\r\nThis code expires with your verification request (within 10 minutes). If you did not request it, ignore this message."))
		return e
	})
	return out, e
}
