package managementwire

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

func modelID(id string) bool {
	if len(id) == 0 || len(id) > 256 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._:/@+-", c)) {
			return false
		}
	}
	return true
}

func parse(ctx context.Context, input Request, body []byte) Result {
	invalid := Result{Reason: "invalid_response"}
	if !supported(input) || input.Action != "discovery" && input.Action != "test" || !strictJSON(ctx, body) {
		return invalid
	}
	if input.Action == "discovery" {
		var response struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &response) != nil || response.Data == nil || len(response.Data) > 1000 {
			return invalid
		}
		items := make([]string, 0, len(response.Data))
		seen := make(map[string]bool)
		for _, item := range response.Data {
			if !modelID(item.ID) {
				return invalid
			}
			if !seen[item.ID] {
				items = append(items, item.ID)
				seen[item.ID] = true
			}
		}
		return Result{Items: items}
	}
	switch input.Protocol {
	case chatProtocol:
		var response struct {
			Choices []struct {
				Index        *int                           `json:"index"`
				Message      struct{ Role, Content string } `json:"message"`
				FinishReason string                         `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal(body, &response) != nil || len(response.Choices) != 1 {
			return invalid
		}
		choice := response.Choices[0]
		if choice.Index == nil || *choice.Index != 0 || choice.Message.Role != "assistant" || strings.TrimSpace(choice.Message.Content) == "" || strings.ContainsRune(choice.Message.Content, 0) || strings.ContainsRune(choice.Message.Content, utf8.RuneError) || len(choice.Message.Content) > 64<<10 || choice.FinishReason != "stop" && choice.FinishReason != "length" {
			return invalid
		}
		return Result{}
	case embedProtocol:
		var response struct {
			Data []struct {
				Index     *int              `json:"index"`
				Embedding []json.RawMessage `json:"embedding"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &response) != nil || len(response.Data) != 1 {
			return invalid
		}
		item := response.Data[0]
		if item.Index == nil || *item.Index != 0 || len(item.Embedding) == 0 || len(item.Embedding) > 4096 {
			return invalid
		}
		for _, raw := range item.Embedding {
			var value float64
			if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil || math.IsInf(value, 0) || math.IsNaN(value) {
				return invalid
			}
		}
		return Result{}
	case rerankProtocol:
		var response struct {
			Results []struct {
				Index          *int     `json:"index"`
				RelevanceScore *float64 `json:"relevance_score"`
			} `json:"results"`
		}
		// The fixed probe supplies two candidates and requests exactly one result.
		// Its single original index is therefore unique by construction.
		if json.Unmarshal(body, &response) != nil || len(response.Results) != 1 {
			return invalid
		}
		item := response.Results[0]
		if item.Index == nil || *item.Index < 0 || *item.Index >= 2 || item.RelevanceScore == nil || math.IsNaN(*item.RelevanceScore) || math.IsInf(*item.RelevanceScore, 0) {
			return invalid
		}
		return Result{}
	case imageProtocol:
		var response struct {
			Data []struct {
				Base64 string `json:"b64_json"`
				URL    string `json:"url"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &response) != nil || len(response.Data) != 1 {
			return invalid
		}
		item := response.Data[0]
		if item.Base64 == "" && item.URL == "" {
			return invalid
		}
		if item.Base64 != "" {
			decoded, err := base64.StdEncoding.Strict().DecodeString(item.Base64)
			valid := err == nil && len(decoded) > 0
			clear(decoded)
			if !valid {
				return invalid
			}
		}
		if item.URL != "" {
			// Validate the shape only. Never fetch or retain a vendor image URL.
			target, err := outbound.ParseTarget(item.URL)
			if err != nil || target.Origin().Scheme() != "https" {
				return invalid
			}
		}
		return Result{}
	default:
		return invalid
	}
}

// Unknown vendor fields are ignored, while duplicate fields and excessive
// nesting are rejected before typed decoding can silently replace a value.
func strictJSON(ctx context.Context, body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 32 || ctx.Err() != nil {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delim, composite := token.(json.Delim)
		if !composite {
			return true
		}
		switch delim {
		case '{':
			seen := make(map[string]bool)
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !value(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim('}')
		case '[':
			for decoder.More() {
				if !value(depth + 1) {
					return false
				}
			}
			end, err := decoder.Token()
			return err == nil && end == json.Delim(']')
		default:
			return false
		}
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}
