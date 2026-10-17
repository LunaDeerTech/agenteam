// Package managementwire implements bounded, fixed provider management probes.
// Authorization, configuration locks and credential leases belong to its caller.
package managementwire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/media"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const (
	discoveryLimit = 2 << 20
	testLimit      = 1 << 20
	imageTestLimit = 8 << 20
	chatProtocol   = "openai-chat-completions"
	embedProtocol  = "openai-embeddings"
	rerankProtocol = "jina-rerank"
	imageProtocol  = "openai-images-generations"
)

var errConfiguration = errors.New("MODEL_MANAGEMENT_WIRE_CONFIGURATION")

type Transport struct {
	Policy   *outbound.PolicyService
	Trust    outbound.TrustStore
	Resolver outbound.Resolver
}

type Request struct {
	Action, Protocol, Endpoint, Model string
	Chat                              *ChatInput
	Embedding                         *mc.EmbeddingCapabilities
}

type ChatInput struct {
	Text         string
	Attachments  []*media.Prepared
	Capabilities mc.Capabilities
}

func (ChatInput) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "management_chat_input") }
func (ChatInput) LogValue() slog.Value       { return slog.StringValue("management_chat_input") }

type Result struct {
	Items      []string
	Reason     string
	Answer     string
	Truncated  bool
	fullAnswer string // only for credential-reflection checking before return
}

func (Result) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "management_result") }
func (Result) LogValue() slog.Value       { return slog.StringValue("management_result") }

// Timeout is the bounded operation budget for this fixed management request.
// Callers use the effective protocol of their authorized, saved model.
func Timeout(input Request) time.Duration {
	if input.Action == "test" && (input.Protocol == imageProtocol || input.Protocol == chatProtocol && input.Chat != nil && len(input.Chat.Attachments) > 0) {
		return 120 * time.Second
	}
	return 20 * time.Second
}

func probeLimits(input Request, responseLimit int) outbound.Limits {
	headers, idle := 15*time.Second, 5*time.Second
	if Timeout(input) == 120*time.Second {
		headers, idle = 120*time.Second, 30*time.Second
	}
	requestLimit := int64(4096)
	if input.Chat != nil {
		requestLimit = media.MaxRequestBytes
	}
	return outbound.Limits{Overall: Timeout(input), ResponseHeaders: headers, ReadIdle: idle,
		RequestHeaderBytes: 16 << 10, ResponseHeaderBytes: 32 << 10,
		RequestBodyBytes: requestLimit, ResponseBodyBytes: int64(responseLimit)}
}

// Run never takes ownership of credential. A nil pointer explicitly selects an
// anonymous provider; unavailable non-nil material cannot downgrade to anonymous.
func (t Transport) Run(ctx context.Context, input Request, credential *sc.SecretMaterial, call outbound.CallContext) (result Result, err error) {
	if ctx == nil {
		return Result{}, errConfiguration
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout(input))
	defer cancel()
	if ctx.Err() != nil {
		return Result{Reason: failure(ctx, ctx.Err())}, nil
	}
	if !supported(input) {
		return Result{Reason: "unsupported_protocol"}, nil
	}
	request, limit, err := prepareContext(ctx, input)
	if err != nil {
		return Result{}, errConfiguration
	}
	defer request.Body.Close()
	var binding outbound.CredentialBinding
	if credential != nil {
		if credential.Use(func([]byte) error { return nil }) != nil {
			return Result{}, errConfiguration
		}
		field, e := outbound.HeaderCredential("Authorization", "Bearer ", *credential)
		if e != nil {
			return Result{}, errConfiguration
		}
		target, e := outbound.ParseTarget(request.URL.String())
		if e != nil {
			return Result{}, errConfiguration
		}
		binding, e = outbound.NewCredentialBinding(target.Origin(), field)
		if e != nil {
			return Result{}, errConfiguration
		}
	}
	profile, err := outbound.NewProfile(outbound.ProfileOptions{
		Consumer: ac.Model, Context: call, Credentials: binding, NoRedirects: true,
		Limits: probeLimits(input, limit),
	})
	if err != nil {
		return Result{}, errConfiguration
	}
	client, err := outbound.NewClient(t.Policy, t.Trust, t.Resolver)
	if err != nil {
		return Result{}, errConfiguration
	}
	// Every synchronous Do/body path has returned before this defer runs. Close
	// the remaining owned sockets and wait for actual transport completion even
	// after the business deadline; cancellation is never a substitute for join.
	defer func() {
		client.StopAdmission()
		if e := client.ForceClose(context.WithoutCancel(ctx)); e != nil {
			result, err = Result{}, errConfiguration
		}
	}()
	response, err := client.Do(ctx, request, profile)
	if err != nil {
		return Result{Reason: failure(ctx, err)}, nil
	}
	defer response.Close()
	if response.StatusCode() < 200 || response.StatusCode() >= 300 {
		return Result{Reason: "upstream_rejected"}, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body(), int64(limit)+1))
	defer clear(body)
	if err != nil {
		return Result{Reason: failure(ctx, err)}, nil
	}
	if len(body) > limit {
		return Result{Reason: "response_limit"}, nil
	}
	result = parse(ctx, input, body)
	if result.Reason == "" && credential != nil && (len(result.Items) != 0 || result.fullAnswer != "") {
		if credential.Use(func(secret []byte) error {
			if bytes.Contains([]byte(result.fullAnswer), secret) {
				result = Result{Reason: "invalid_response"}
				return nil
			}
			for _, item := range result.Items {
				if bytes.Contains([]byte(item), secret) {
					result = Result{Reason: "invalid_response"}
					break
				}
			}
			return nil
		}) != nil {
			return Result{}, errConfiguration
		}
	}
	result.fullAnswer = ""
	if ctx.Err() != nil {
		return Result{Reason: failure(ctx, ctx.Err())}, nil
	}
	return result, nil
}

func supported(input Request) bool {
	switch input.Protocol {
	case chatProtocol, embedProtocol:
		return true
	case rerankProtocol, imageProtocol:
		return input.Action == "test"
	default:
		return false
	}
}

func prepare(input Request) (*http.Request, int, error) {
	return prepareContext(context.Background(), input)
}

func prepareContext(ctx context.Context, input Request) (*http.Request, int, error) {
	if ctx == nil || ctx.Err() != nil || input.Action != "discovery" && input.Action != "test" || !supported(input) || len(input.Endpoint) > 8192 || input.Chat != nil && (input.Action != "test" || input.Protocol != chatProtocol) {
		return nil, 0, errConfiguration
	}
	u, err := url.Parse(input.Endpoint)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || strings.Contains(input.Endpoint, "#") {
		return nil, 0, errConfiguration
	}
	if _, err = outbound.ParseTarget(input.Endpoint); err != nil {
		return nil, 0, errConfiguration
	}
	suffix, method, limit := "/models", http.MethodGet, discoveryLimit
	var body []byte
	if input.Action == "test" {
		if !modelID(input.Model) {
			return nil, 0, errConfiguration
		}
		method, limit = http.MethodPost, testLimit
		switch input.Protocol {
		case chatProtocol:
			suffix = "/chat/completions"
			var content any = "Reply OK."
			maxTokens := 16
			if input.Chat != nil {
				content, err = adapter.EncodeUserContent(ctx, input.Chat.Text, input.Chat.Attachments, input.Chat.Capabilities)
				if err != nil {
					return nil, 0, errConfiguration
				}
				maxTokens = 2048
			}
			body, err = json.Marshal(struct {
				Model               string           `json:"model"`
				Messages            []map[string]any `json:"messages"`
				MaxCompletionTokens int              `json:"max_completion_tokens"`
				Stream              bool             `json:"stream"`
			}{input.Model, []map[string]any{{"role": "user", "content": content}}, maxTokens, false})
		case embedProtocol:
			if input.Embedding == nil || input.Embedding.Validate() != nil {
				return nil, 0, errConfiguration
			}
			var dimensions *int64
			if input.Embedding.DimensionsMode == mc.EmbeddingRequestDimensions {
				value := int64(input.Embedding.Dimensions)
				dimensions = &value
			}
			suffix = "/embeddings"
			body, err = json.Marshal(struct {
				Model          string `json:"model"`
				Input          string `json:"input"`
				EncodingFormat string `json:"encoding_format"`
				Dimensions     *int64 `json:"dimensions,omitempty"`
			}{Model: input.Model, Input: "Connection test.", EncodingFormat: "float", Dimensions: dimensions})
		case rerankProtocol:
			suffix = "/rerank"
			body, err = json.Marshal(struct {
				Model           string   `json:"model"`
				Query           string   `json:"query"`
				Documents       []string `json:"documents"`
				TopN            int      `json:"top_n"`
				ReturnDocuments bool     `json:"return_documents"`
			}{input.Model, "Which document mentions a cat?", []string{"A cat is resting.", "A tree is growing."}, 1, false})
		case imageProtocol:
			suffix, limit = "/images/generations", imageTestLimit
			body, err = json.Marshal(struct {
				Model  string `json:"model"`
				Prompt string `json:"prompt"`
				N      int    `json:"n"`
			}{input.Model, "A plain blue circle on a white background.", 1})
		default:
			return nil, 0, errConfiguration
		}
		if err != nil || len(body) > media.MaxRequestBytes {
			return nil, 0, errConfiguration
		}
	}
	// Build from EscapedPath so an encoded slash in a saved provider prefix is
	// preserved as an encoded slash, not silently converted into a separator.
	u.RawPath = strings.TrimRight(u.EscapedPath(), "/") + suffix
	u.Path, err = url.PathUnescape(u.RawPath)
	if err != nil {
		return nil, 0, errConfiguration
	}
	request, err := http.NewRequest(method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, 0, errConfiguration
	}
	if request.Body == nil {
		request.Body = http.NoBody
	}
	request.GetBody = nil
	request.Close = true
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	return request, limit, nil
}

func failure(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	switch outbound.SafeNetworkError(err).Decision().Reason {
	case ac.Timeout:
		return "timeout"
	case ac.Cancelled:
		return "cancelled"
	case ac.TLSFailed:
		return "tls_failed"
	case ac.ResponseLimit:
		return "response_limit"
	case ac.RedirectDenied:
		return "upstream_rejected"
	case ac.InvalidTarget, ac.AddressForbidden, ac.RuleNotAllowed, ac.PrivateNotAllowed, ac.PortDenied, ac.HTTPDenied, ac.PolicyUnavailable, ac.BindingInvalid, ac.ConsumerDenied, ac.CredentialDenied, ac.OriginDenied:
		return "policy_rejected"
	default:
		return "network_failed"
	}
}
