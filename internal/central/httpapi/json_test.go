package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type childDTO struct {
	Name string `json:"name"`
}
type inputDTO struct {
	Name            string                 `json:"name"`
	Version         foundation.Version     `json:"version"`
	ExpectedVersion *foundation.Version    `json:"expected_version"`
	Count           int64                  `json:"count"`
	Float           float64                `json:"float"`
	Data            any                    `json:"data"`
	Nested          *childDTO              `json:"nested"`
	List            []childDTO             `json:"list"`
	Raw             json.RawMessage        `json:"raw"`
	Page            foundation.PageRequest `json:"page"`
}

func decodeInput(body string, dst any, max int64) error {
	r := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return DecodeJSON(httptest.NewRecorder(), r, dst, max)
}

func requireFault(t *testing.T, err error, code foundation.Code, state foundation.CommitState) {
	t.Helper()
	var fault *foundation.Fault
	if !errors.As(err, &fault) || fault.Code != code || fault.CommitState != state {
		t.Fatalf("want %s/%s, got %v", code, state, err)
	}
}

func TestDecodeJSONRejectsAmbiguousInput(t *testing.T) {
	for name, body := range map[string]string{
		"empty": "", "whitespace": " \n\t", "array": `[]`, "null": `null`, "string": `"x"`, "number": `1`, "bool": `true`,
		"malformed": `{"name":}`, "garbage": `{"name":"ok"}junk`, "second": `{} {}`, "trailing_comma": `{"name":"ok",}`,
		"duplicate": `{"name":"one","name":"two"}`, "escaped_duplicate": `{"name":"one","n\u0061me":"two"}`,
		"nested_duplicate": `{"data":{"x":1,"x":2}}`, "array_duplicate": `{"data":[{"x":1,"x":2}]}`,
		"raw_duplicate": `{"raw":{"x":1,"x":2}}`, "case_alias": `{"Name":"x"}`, "unknown": `{"secret":"x"}`,
		"nested_alias": `{"nested":{"Name":"x"}}`, "nested_unknown": `{"nested":{"missing":"x"}}`,
		"array_alias": `{"list":[{"NAME":"x"}]}`, "null_scalar": `{"version":null}`, "number_scalar": `{"version":1}`,
		"noncanonical_scalar": `{"version":"01"}`, "zero_scalar": `{"version":"0"}`, "overflow_scalar": `{"version":"9223372036854775808"}`,
		"overflow_int": `{"count":9223372036854775808}`, "null_int": `{"count":null}`, "null_string": `{"name":null}`,
		"lossy_float": `{"float":9007199254740993}`, "lossy_float_exponent": `{"float":9007199254740993e0}`,
		"nonfinite_float": `{"float":1e9999}`, "nan": `{"float":NaN}`, "infinity": `{"float":Infinity}`,
		"underflow_float": `{"float":1e-999999999}`, "huge_float_exponent": `{"float":1e999999999}`,
		"page_alias": `{"page":{"Limit":100}}`, "page_invalid": `{"page":{"limit":0}}`,
		"invalid_utf8": "{\"name\":\"" + string([]byte{0xff}) + "\"}",
	} {
		t.Run(name, func(t *testing.T) {
			input := inputDTO{Name: "unchanged"}
			err := decodeInput(body, &input, 0)
			requireFault(t, err, foundation.InvalidArgument, foundation.NotStarted)
			if input.Name != "unchanged" {
				t.Fatal("failed decode mutated destination")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("decoder input leaked")
			}
		})
	}
}

func TestDecodeJSONPreservesPreciseTypes(t *testing.T) {
	body := `{"name":"正文","version":"9223372036854775807","expected_version":null,"count":9223372036854775807,"float":1.5,"data":{"integer":90071992547409931234567890},"nested":{"name":"n"},"list":[{"name":"l"}],"page":{}}`
	var input inputDTO
	if err := decodeInput(body, &input, 0); err != nil {
		t.Fatal(err)
	}
	if input.Count != math.MaxInt64 || input.Version != foundation.Version(math.MaxInt64) || input.ExpectedVersion != nil || input.Page.Limit != 50 {
		t.Fatalf("wrong scalar projection: %+v", input)
	}
	number, ok := input.Data.(map[string]any)["integer"].(json.Number)
	if !ok || number.String() != "90071992547409931234567890" {
		t.Fatal("dynamic integer lost precision")
	}
	for _, body := range []string{`{"float":9007199254740992}`, `{"float":1.0}`, `{"float":1e0}`, `{"float":0e-999999999}`, `{"name":"x"} \n`} {
		if strings.HasSuffix(body, `\n`) {
			body = strings.TrimSuffix(body, `\n`) + "\n"
		}
		if err := decodeInput(body, &input, 0); err != nil {
			t.Fatalf("valid input %s: %v", body, err)
		}
	}
}

func TestDecodeJSONDepthAndByteLimits(t *testing.T) {
	for _, depth := range []int{63, 64} {
		body := `{"data":` + strings.Repeat("[", depth) + `0` + strings.Repeat("]", depth) + `}`
		err := decodeInput(body, new(inputDTO), 0)
		if depth == 63 && err != nil {
			t.Fatal("64 container levels should fit", err)
		}
		if depth == 64 {
			requireFault(t, err, foundation.InvalidArgument, foundation.NotStarted)
		}
	}
	valid := `{"name":"abcdefgh"}`
	if err := decodeInput(valid, new(inputDTO), int64(len(valid))); err != nil {
		t.Fatal("exact byte limit rejected", err)
	}
	requireFault(t, decodeInput(valid, new(inputDTO), int64(len(valid)-1)), foundation.PayloadTooLarge, foundation.NotStarted)
	oversized := `{"name":"` + strings.Repeat("x", int(DefaultMaxJSONBytes)) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(oversized))
	r.Header.Set("Content-Type", "application/json")
	r.ContentLength = 1
	requireFault(t, DecodeJSON(httptest.NewRecorder(), r, new(inputDTO), 0), foundation.PayloadTooLarge, foundation.NotStarted)
}

func TestDecodeJSONMediaAndDestination(t *testing.T) {
	for _, media := range []string{"application/json", "application/json; charset=utf-8", "Application/JSON; charset=UTF-8"} {
		r := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", media)
		if err := DecodeJSON(httptest.NewRecorder(), r, new(inputDTO), 0); err != nil {
			t.Errorf("accepted media %s: %v", media, err)
		}
	}
	for _, media := range []string{"", "text/plain", "application/problem+json", "application/json; charset=latin1", "application/json; bad=x", "application/json,"} {
		r := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", media)
		requireFault(t, DecodeJSON(httptest.NewRecorder(), r, new(inputDTO), 0), foundation.UnsupportedMediaType, foundation.NotStarted)
	}
	for _, encoding := range []string{"gzip", "br", "identity"} {
		r := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Content-Encoding", encoding)
		requireFault(t, DecodeJSON(httptest.NewRecorder(), r, new(inputDTO), 0), foundation.UnsupportedMediaType, foundation.NotStarted)
	}
	r := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(`{}`))
	r.Header.Add("Content-Type", "application/json")
	r.Header.Add("Content-Type", "application/json")
	requireFault(t, DecodeJSON(httptest.NewRecorder(), r, new(inputDTO), 0), foundation.UnsupportedMediaType, foundation.NotStarted)
	for _, dst := range []any{nil, (*inputDTO)(nil), inputDTO{}, new(map[string]any), new([]int), &struct{ Untagged string }{}} {
		requireFault(t, decodeInput(`{}`, dst, 0), foundation.InternalError, foundation.Unknown)
	}
	requireFault(t, decodeInput(`{}`, new(inputDTO), -1), foundation.InternalError, foundation.Unknown)
}

func TestChunkedJSONLimitOnRealHTTP(t *testing.T) {
	chunked := make(chan bool, 1)
	server := httptest.NewServer(Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunked <- r.ContentLength == -1 && len(r.TransferEncoding) == 1 && r.TransferEncoding[0] == "chunked"
		if err := DecodeJSON(w, r, new(inputDTO), 32); err != nil {
			WriteProblem(w, r, err)
			return
		}
		_ = WriteJSON(w, r, 200, struct{}{})
	})))
	defer server.Close()
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(writer, strings.NewReader(`{"name":"`+strings.Repeat("x", 256)+`"}`))
		_ = writer.CloseWithError(err)
		done <- err
	}()
	req, err := http.NewRequest(http.MethodPost, server.URL, reader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	b, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 413 || !bytes.Contains(b, []byte(`"code":"PAYLOAD_TOO_LARGE"`)) || !<-chunked {
		t.Fatalf("chunked limit: %d %s", response.StatusCode, b)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
