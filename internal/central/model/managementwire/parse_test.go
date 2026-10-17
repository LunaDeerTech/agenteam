package managementwire

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"
)

const validChat = `{"choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`
const validEmbedding = `{"data":[{"index":0,"embedding":[0,0.25,-1.5]}]}`

func TestParsersValidateOnlyTheirProtocol(t *testing.T) {
	tests := []struct {
		name, action, protocol, body, reason string
		items                                []string
	}{
		{"discovery", "discovery", chatProtocol, `{"data":[{"id":"vendor/model:1","owner":"ignored"},{"id":"embedding-looking-name"},{"id":"vendor/model:1"}]}`, "", []string{"vendor/model:1", "embedding-looking-name"}},
		{"empty-list", "discovery", embedProtocol, `{"data":[]}`, "", []string{}},
		{"missing-data", "discovery", chatProtocol, `{}`, "invalid_response", nil},
		{"null-data", "discovery", chatProtocol, `{"data":null}`, "invalid_response", nil},
		{"missing-id", "discovery", chatProtocol, `{"data":[{}]}`, "invalid_response", nil},
		{"empty-id", "discovery", chatProtocol, `{"data":[{"id":""}]}`, "invalid_response", nil},
		{"control-id", "discovery", chatProtocol, `{"data":[{"id":"a\nb"}]}`, "invalid_response", nil},
		{"markup-id", "discovery", chatProtocol, `{"data":[{"id":"<script>"}]}`, "invalid_response", nil},
		{"long-id", "discovery", chatProtocol, `{"data":[{"id":"` + strings.Repeat("a", 257) + `"}]}`, "invalid_response", nil},
		{"duplicate-key", "discovery", chatProtocol, `{"data":[],"data":[{"id":"replacement"}]}`, "invalid_response", nil},
		{"invalid-surrogate", "discovery", chatProtocol, `{"data":[{"id":"\ud800"}]}`, "invalid_response", nil},
		{"trailing-value", "discovery", chatProtocol, `{"data":[]} {}`, "invalid_response", nil},
		{"chat", "test", chatProtocol, validChat, "", nil},
		{"chat-empty", "test", chatProtocol, strings.Replace(validChat, `"OK"`, `" "`, 1), "invalid_response", nil},
		{"chat-wrong-role", "test", chatProtocol, strings.Replace(validChat, "assistant", "user", 1), "invalid_response", nil},
		{"chat-wrong-index", "test", chatProtocol, strings.Replace(validChat, `"index":0`, `"index":1`, 1), "invalid_response", nil},
		{"chat-missing-index", "test", chatProtocol, strings.Replace(validChat, `"index":0,`, "", 1), "invalid_response", nil},
		{"chat-content-filter", "test", chatProtocol, strings.Replace(validChat, "stop", "content_filter", 1), "invalid_response", nil},
		{"embedding", "test", embedProtocol, validEmbedding, "", nil},
		{"embedding-null", "test", embedProtocol, `{"data":[{"index":0,"embedding":[null]}]}`, "invalid_response", nil},
		{"embedding-string", "test", embedProtocol, `{"data":[{"index":0,"embedding":["1"]}]}`, "invalid_response", nil},
		{"embedding-overflow", "test", embedProtocol, `{"data":[{"index":0,"embedding":[1e999]}]}`, "invalid_response", nil},
		{"embedding-empty", "test", embedProtocol, `{"data":[{"index":0,"embedding":[]}]}`, "invalid_response", nil},
		{"embedding-index", "test", embedProtocol, strings.Replace(validEmbedding, `"index":0`, `"index":1`, 1), "invalid_response", nil},
		{"embedding-duplicates", "test", embedProtocol, `{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[1]}]}`, "invalid_response", nil},
		{"wrong-protocol", "test", chatProtocol, validEmbedding, "invalid_response", nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := parse(context.Background(), Request{Action: test.action, Protocol: test.protocol}, []byte(test.body))
			if result.Reason != test.reason || !reflect.DeepEqual(result.Items, test.items) {
				t.Fatalf("safe result mismatch: reason=%s count=%d", result.Reason, len(result.Items))
			}
		})
	}
	items := make([]map[string]string, 1001)
	for i := range items {
		items[i] = map[string]string{"id": "same"}
	}
	body, _ := json.Marshal(map[string]any{"data": items})
	if parse(context.Background(), Request{Action: "discovery"}, body).Reason != "invalid_response" {
		t.Fatal("oversized discovery list accepted")
	}
	vector := strings.Repeat("0,", 4096) + "0"
	if parse(context.Background(), Request{Action: "test", Protocol: embedProtocol}, []byte(`{"data":[{"index":0,"embedding":[`+vector+`]}]}`)).Reason != "invalid_response" {
		t.Fatal("oversized vector accepted")
	}
	if strictJSON(context.Background(), []byte(strings.Repeat("[", 34)+"0"+strings.Repeat("]", 34))) {
		t.Fatal("excessive nesting accepted")
	}
}

func TestPreparePreservesEscapedPrefixAndFixedPayload(t *testing.T) {
	for _, protocol := range []string{chatProtocol, embedProtocol} {
		request, limit, err := prepare(Request{Action: "test", Protocol: protocol, Endpoint: "https://fixture.test/v1%2Ftenant///", Model: "vendor/model:1"})
		if err != nil {
			t.Fatal(err)
		}
		defer request.Body.Close()
		wantPath := "/v1%2Ftenant/chat/completions"
		if protocol == embedProtocol {
			wantPath = "/v1%2Ftenant/embeddings"
		}
		if request.URL.EscapedPath() != wantPath || request.Method != "POST" || request.GetBody != nil || !request.Close || limit != testLimit {
			t.Fatal("fixed request contract changed")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		var actual map[string]any
		if json.Unmarshal(body, &actual) != nil || actual["model"] != "vendor/model:1" {
			t.Fatal("model JSON field missing")
		}
		if protocol == chatProtocol {
			if len(actual) != 4 || actual["max_completion_tokens"] != float64(16) || actual["stream"] != false {
				t.Fatal("chat probe is not bounded")
			}
			messages, ok := actual["messages"].([]any)
			if !ok || len(messages) != 1 || !reflect.DeepEqual(messages[0], map[string]any{"role": "user", "content": "Reply OK."}) {
				t.Fatal("chat probe content changed")
			}
		} else if len(actual) != 3 || actual["input"] != "Connection test." || actual["encoding_format"] != "float" {
			t.Fatal("embedding probe body changed")
		}
	}
	for _, endpoint := range []string{"https://fixture.test/?key=x", "https://fixture.test/?", "https://user:pass@fixture.test/", "https://fixture.test/#", "ftp://fixture.test/"} {
		if _, _, err := prepare(Request{Action: "discovery", Endpoint: endpoint}); err == nil {
			t.Fatal("unsafe saved endpoint accepted")
		}
	}
}
