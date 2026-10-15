// The owned acceptance provider is a real official-SDK MCP server. It does not
// implement a fake client or replace any production transport/authorization.
package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	cert := flag.String("cert", "", "TLS certificate")
	key := flag.String("key", "", "TLS key")
	nonce := flag.String("nonce", "", "fixture ownership nonce")
	tokenFile := flag.String("token-file", "", "owned generated token file")
	flag.Parse()
	token, err := os.ReadFile(*tokenFile)
	if err != nil {
		os.Exit(2)
	}
	var calls atomic.Int64
	mux := http.NewServeMux()
	for _, profile := range []struct {
		path, revision string
		stateless      bool
	}{{"/modern", "2026-07-28", true}, {"/legacy", "2025-11-25", false}} {
		server := sdk.NewServer(&sdk.Implementation{Name: "agenteam-owned-mcp", Version: "1"}, &sdk.ServerOptions{PageSize: 1, SupportedProtocolVersions: []string{profile.revision}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
		if profile.path == "/modern" {
			registerMedia(server)
		}
		server.AddTool(&sdk.Tool{Name: "echo", Description: "Return the supplied text from the owned provider", InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`), Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, r *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			calls.Add(1)
			var args struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(r.Params.Arguments, &args) != nil {
				return nil, json.Unmarshal(r.Params.Arguments, &args)
			}
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "provider:" + args.Text}}}, nil
		})
		server.AddTool(&sdk.Tool{Name: "resource_link", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false}`), Annotations: &sdk.ToolAnnotations{ReadOnlyHint: true}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			calls.Add(1)
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.ResourceLink{URI: "repo://owned/data", Name: "owned-data", MIMEType: "text/plain"}}}, nil
		})
		server.AddTool(&sdk.Tool{Name: "failure", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			calls.Add(1)
			return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "owned_provider_rejected"}}}, nil
		})
		server.AddTool(&sdk.Tool{Name: "reflect_credential", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			calls.Add(1)
			return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(token)}}}, nil
		})
		server.AddTool(&sdk.Tool{Name: "wait", InputSchema: json.RawMessage(`{"type":"object"}`)}, func(ctx context.Context, _ *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			calls.Add(1)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		server.AddResource(&sdk.Resource{URI: "repo://owned/data", Name: "owned-data", MIMEType: "text/plain"}, func(context.Context, *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: "repo://owned/data", MIMEType: "text/plain", Text: "real MCP resource bytes"}}}, nil
		})
		server.AddResourceTemplate(&sdk.ResourceTemplate{URITemplate: "repo://owned/item/{name}", Name: "owned-item", MIMEType: "text/plain"}, func(_ context.Context, r *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: r.Params.URI, MIMEType: "text/plain", Text: "template bytes"}}}, nil
		})
		handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: profile.stateless, JSONResponse: true})
		if profile.path == "/modern" {
			registerOAuth(mux, handler, *nonce)
		}
		wrap := func(protected bool) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				if protected && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), append([]byte("Bearer "), token...)) != 1 {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				if r.Method == "POST" {
					body, e := io.ReadAll(io.LimitReader(r.Body, 1<<20))
					_ = r.Body.Close()
					if e != nil {
						w.WriteHeader(400)
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(body))
					var msg struct {
						Method string `json:"method"`
						Params struct {
							Name string `json:"name"`
						} `json:"params"`
					}
					_ = json.Unmarshal(body, &msg)
					if msg.Method == "tools/call" && msg.Params.Name == "unknown" {
						calls.Add(1)
						if hijack, ok := w.(http.Hijacker); ok {
							conn, _, e := hijack.Hijack()
							if e == nil {
								_ = conn.Close()
							}
						}
						return
					}
				}
				handler.ServeHTTP(w, r)
			}
		}
		mux.Handle(profile.path, wrap(false))
		mux.Handle(profile.path+"/protected", wrap(true))
	}
	mux.HandleFunc("/control", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Fixture-Nonce")), []byte(*nonce)) != 1 {
			w.WriteHeader(403)
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			Calls int64 `json:"calls"`
		}{calls.Load()})
	})
	server := &http.Server{Addr: ":8443", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServeTLS(*cert, *key) }()
	select {
	case <-ctx.Done():
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(drain)
		<-done
	case err := <-done:
		if err != http.ErrServerClosed {
			os.Exit(1)
		}
	}
}
