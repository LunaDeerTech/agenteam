package parser_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	p "github.com/LunaDeerTech/agenteam/internal/central/retrieval/parser"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func key[T any](n int) f.ID[T] {
	return must(f.ParseID[T](fmt.Sprintf("01902e15-1000-7000-8000-%012x", n)))
}

func source() p.SourceIdentity {
	return p.SourceIdentity{ProjectID: key[id.Project](1), DocumentID: key[kc.Document](2), ContentVersion: 9007199254740993, ObjectID: key[oc.StoredObject](3)}
}

func wantFault(t *testing.T, result p.StructuredDocument, err error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code || fault.CommitState != f.NotStarted {
		t.Fatalf("want %s/not_started, got %v", code, err)
	}
	if !reflect.DeepEqual(result, p.StructuredDocument{}) {
		t.Fatal("failure exposed a partial result")
	}
}

func TestSourceIdentityUsesExistingMandatoryFields(t *testing.T) {
	valid := source()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*p.SourceIdentity){
		func(s *p.SourceIdentity) { s.ProjectID = id.ProjectID{} },
		func(s *p.SourceIdentity) { s.DocumentID = kc.DocumentID{} },
		func(s *p.SourceIdentity) { s.ObjectID = oc.ObjectID{} },
		func(s *p.SourceIdentity) { s.ContentVersion = 0 },
	} {
		bad := valid
		edit(&bad)
		got, err := p.ParsePlainText(context.Background(), bad, "valid content")
		wantFault(t, got, err, f.InvalidArgument)
	}
	got := must(p.ParsePlainText(context.Background(), valid, "value"))
	if got.Source != valid || got.ParserProfile != "plain_text:v1" {
		t.Fatal("lost exact source identity, large integer version or profile")
	}
}

func TestParserLoggingAndErrorsDoNotExposeText(t *testing.T) {
	const canary = "private-parser-content-canary"
	got := must(p.ParsePlainText(context.Background(), source(), canary))
	var log bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&log, nil))
	logger.Info("derived", "document", got, "element", got.Elements[0])
	formatted := fmt.Sprintf("%+v %#v %+v", got, &got, got.Elements)
	if strings.Contains(log.String()+formatted, canary) {
		t.Fatal("implicit logging exposed content")
	}
	bad, err := p.ParsePlainText(context.Background(), source(), canary+"\xff")
	wantFault(t, bad, err, f.InvalidArgument)
	if strings.Contains(fmt.Sprintf("%+v %#v", err, err), canary) {
		t.Fatal("parse error exposed input")
	}
}
