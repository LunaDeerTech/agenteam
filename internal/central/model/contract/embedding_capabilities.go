package contract

import (
	"encoding/json"
	"strconv"
)

// EmbeddingDimensions counts vector components, not tokens. Its JSON form is
// an exact decimal string, like other bounded integer Model configuration.
type EmbeddingDimensions int64

func (d EmbeddingDimensions) Validate() error {
	if d < 1 || d > 4096 {
		return bad()
	}
	return nil
}

func (d EmbeddingDimensions) MarshalJSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(strconv.FormatInt(int64(d), 10))
}

func (d *EmbeddingDimensions) UnmarshalJSON(raw []byte) error {
	var text *string
	if d == nil || json.Unmarshal(raw, &text) != nil || text == nil {
		return bad()
	}
	n, err := strconv.ParseInt(*text, 10, 64)
	value := EmbeddingDimensions(n)
	if err != nil || strconv.FormatInt(n, 10) != *text || value.Validate() != nil {
		return bad()
	}
	*d = value
	return nil
}

const (
	EmbeddingFixedDimensions   = "fixed"
	EmbeddingRequestDimensions = "request"
)

// EmbeddingCapabilities defines the vector space used by this configuration.
// Fixed validates the provider's output without asking it to resize. Request
// additionally sends the explicit dimensions field through a supported adapter.
type EmbeddingCapabilities struct {
	Dimensions     EmbeddingDimensions `json:"dimensions"`
	DimensionsMode string              `json:"dimensions_mode"`
}

func (e EmbeddingCapabilities) Validate() error {
	if e.Dimensions.Validate() != nil || !one(e.DimensionsMode, EmbeddingFixedDimensions, EmbeddingRequestDimensions) {
		return bad()
	}
	return nil
}

// ValidateForModelType prevents non-embedding models from carrying a vector
// configuration and prevents an embedding model from guessing its output size.
func (c Capabilities) ValidateForModelType(typ ModelType) error {
	if !typ.Valid() || c.Validate() != nil || (typ == EmbeddingModel) != (c.Embedding != nil) {
		return bad()
	}
	return nil
}
