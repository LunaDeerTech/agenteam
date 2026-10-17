package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestEmbeddingHTTPExactConfiguration(t *testing.T) {
	base := strings.ReplaceAll(httpValidModel, `"type":"chat"`, `"type":"embedding"`)
	base = strings.ReplaceAll(base, `"streaming":true`, `"streaming":false`)
	base = strings.ReplaceAll(base, `"output_modalities":["text"]`, `"output_modalities":["vector"]`)
	object := `{"dimensions":"1536","dimensions_mode":"fixed"}`
	valid := strings.Replace(base, `"embedding":null`, `"embedding":`+object, 1)
	var dto httpModelInput
	if err := decodeHTTP(t, valid, &dto); err != nil {
		t.Fatal(err)
	}
	value, err := dto.input()
	if err != nil || modelPolicy(value, mc.OpenAICompatible) != nil || value.Capabilities.Embedding == nil || value.Capabilities.Embedding.Dimensions != 1536 {
		t.Fatal("legal explicit embedding rejected", err)
	}
	for _, bad := range []string{
		strings.Replace(valid, `"embedding":`+object+`,`, "", 1),
		strings.Replace(valid, object, `null`, 1),
		strings.Replace(valid, object, `{}`, 1),
		strings.Replace(valid, `"dimensions":"1536"`, `"dimensions":1536`, 1),
		strings.Replace(valid, `"1536"`, `"01536"`, 1),
		strings.Replace(valid, `"1536"`, `"4097"`, 1),
		strings.Replace(valid, `"dimensions":`, `"Dimensions":`, 1),
		strings.Replace(valid, `"dimensions_mode":`, `"Dimensions_mode":`, 1),
		strings.Replace(valid, `"fixed"`, `"auto"`, 1),
		strings.Replace(valid, object, `{"dimensions":"1536","dimensions_mode":"fixed","extra":true}`, 1),
		strings.Replace(valid, object, `{"dimensions":"1536","dimensions":"2","dimensions_mode":"fixed"}`, 1),
		strings.Replace(httpValidModel, `"embedding":null`, `"embedding":`+object, 1),
	} {
		var rejected httpModelInput
		if decodeHTTP(t, bad, &rejected) == nil {
			input, e := rejected.input()
			if e == nil && modelPolicy(input, mc.OpenAICompatible) == nil {
				t.Fatal("invalid model embedding accepted")
			}
		}
	}
	now, err := f.NewInstant(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(httpModelDTO(mc.ModelView{ID: mustID[mc.Model](t), ProviderID: mustID[mc.Provider](t), Input: value, Version: 1, CreatedAt: now, UpdatedAt: now}))
	if err != nil || !strings.Contains(string(raw), `"embedding":`+object) {
		t.Fatal("list/detail projection lost exact dimensions", err)
	}
}
