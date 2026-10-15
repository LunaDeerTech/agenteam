package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type linkReadFunc func(context.Context, string) ([]protocol.ResourceContent, error)

func (fn linkReadFunc) ReadResource(ctx context.Context, uri string) ([]protocol.ResourceContent, error) {
	return fn(ctx, uri)
}

func TestResourceLinkExpandsSameAdmittedSessionBeforeMaterialization(t *testing.T) {
	checked, reads := 0, 0
	text := "canonical resource body"
	read := linkReadFunc(func(_ context.Context, uri string) ([]protocol.ResourceContent, error) {
		reads++
		if checked != reads || uri != "repo://owned/item" {
			t.Fatal("link escaped its original admitted namespace or current proof")
		}
		return []protocol.ResourceContent{{URI: uri, MIMEType: "text/plain", Text: &text}, {URI: uri + "/binary", MIMEType: "application/octet-stream", Blob: []byte{0, 1, 2}}}, nil
	})
	out, err := expandResourceLinks(context.Background(), read, func(context.Context) error { checked++; return nil }, []protocol.Content{{Kind: "text", Text: "before"}, {Kind: "resource_link", URI: "repo://owned/item"}, {Kind: "text", Text: "after"}}, 256)
	if err != nil || reads != 1 || len(out) != 4 || out[0].Text != "before" || out[1].Kind != "resource" || out[2].Kind != "resource" || out[3].Text != "after" {
		t.Fatal("Resource Adapter lost order or did not materialize the returned contents")
	}
	if out[1].Resource == out[2].Resource || out[1].Resource.Text == nil || *out[1].Resource.Text != text || len(out[2].Resource.Blob) != 3 {
		t.Fatal("expanded contents aliased or fabricated a canonical reference")
	}
}

func TestResourceLinkCurrentAuthorityAndPostprocessingFailure(t *testing.T) {
	reads := 0
	refused := fail(f.Forbidden)
	read := linkReadFunc(func(context.Context, string) ([]protocol.ResourceContent, error) {
		reads++
		return nil, &protocol.Error{Code: protocol.TransportFailure}
	})
	link := []protocol.Content{{Kind: "resource_link", URI: "repo://owned/item"}}
	if out, err := expandResourceLinks(context.Background(), read, func(context.Context) error { return refused }, link, 256); !errors.Is(err, refused) || out != nil || reads != 0 {
		t.Fatal("revoked original invocation reached the SDK read")
	}
	_, err := expandResourceLinks(context.Background(), read, func(context.Context) error { return nil }, link, 256)
	if err == nil || reads != 1 {
		t.Fatal("failed read was retried or omitted")
	}
	result, adapterError := backendKnownFailure(tc.MCPInvocation{Call: tc.ToolCallBinding{CallID: "original-call"}}, err)
	if adapterError != nil || !result.OutcomeKnown || !result.MayHaveExecuted || result.FailureCode != string(protocol.TransportFailure) || result.Result.CallID != "original-call" || result.Result.Validate() != nil {
		t.Fatal("known Tool response became unknown after Resource postprocessing failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := expandResourceLinks(ctx, read, func(context.Context) error { t.Fatal("cancelled invocation requested authority"); return nil }, link, 256); !errors.Is(err, context.Canceled) || reads != 1 {
		t.Fatal("cancelled invocation reached the SDK")
	}
}

func TestResourceLinkAggregateBoundsAndEmptyRead(t *testing.T) {
	link := []protocol.Content{{Kind: "resource_link", URI: "repo://owned/item"}}
	allow := func(context.Context) error { return nil }
	for _, test := range []struct {
		name     string
		maximum  int
		contents []protocol.ResourceContent
		code     protocol.Code
	}{
		{"empty", 256, nil, protocol.InvalidResponse},
		{"parts", 1, []protocol.ResourceContent{{URI: "repo:a"}, {URI: "repo:b"}}, protocol.TooLarge},
		{"bytes", 256, []protocol.ResourceContent{{URI: "repo:a", Blob: []byte(strings.Repeat("a", protocol.MaxContentBytes/2))}, {URI: "repo:b", Blob: []byte(strings.Repeat("b", protocol.MaxContentBytes/2))}}, protocol.TooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, err := expandResourceLinks(context.Background(), linkReadFunc(func(context.Context, string) ([]protocol.ResourceContent, error) { return test.contents, nil }), allow, link, test.maximum)
			var p *protocol.Error
			if out != nil || !errors.As(err, &p) || p.Code != test.code {
				t.Fatal("unbounded or absent Resource content was accepted")
			}
		})
	}
}
