package model

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "testing"

    f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
    id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
    "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
    mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
    sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func independentEmbeddingRequest(t *testing.T, purpose mc.Purpose, user id.UserID) mc.ResolveRequest {
    t.Helper()
    actor, err := id.NewHuman(user, mustID[id.Session](t))
    if err != nil { t.Fatal(err) }
    owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, mustID[mc.Call](t).String())
    if err != nil { t.Fatal(err) }
    consumer := mc.Consumer{Kind: mc.KnowledgeConsumer, Purpose: purpose, ProjectID: mustID[id.Project](t), OperationID: mustID[struct{}](t).String()}
    if purpose == mc.MemoryEmbedding {
        agent := mustID[id.Agent](t)
        consumer.Kind, consumer.AgentID = mc.MemoryConsumer, &agent
    }
    req := mc.ResolveRequest{Actor: actor, Consumer: consumer, Purpose: purpose, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.EmbeddingSelector}, LeaseOwner: owner}
    if err := req.Validate(); err != nil { t.Fatal("independent request is not C0-valid", err) }
    return req
}

func TestIndependentPlatformEmbeddingPolicyComplement(t *testing.T) {
    t.Run("identity_semantic_session", func(t *testing.T) {
        for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
            t.Run(string(purpose), func(t *testing.T) {
                user := mustID[id.User](t)
                original := independentEmbeddingRequest(t, purpose, user)
                originalIdentity, err := resolutionIdentity(original)
                if err != nil { t.Fatal(err) }
                originalBinding, err := mc.ResolveBinding(original)
                if err != nil { t.Fatal(err) }
                originalSemantic := resolutionSemantic(original)
                sessionChanged := original.Clone()
                sessionChanged.Actor, err = id.NewHuman(user, mustID[id.Session](t))
                if err != nil || sessionChanged.Validate() != nil { t.Fatal("new session structure", err) }
                nextIdentity, err := resolutionIdentity(sessionChanged)
                if err != nil { t.Fatal(err) }
                nextBinding, err := mc.ResolveBinding(sessionChanged)
                if err != nil { t.Fatal(err) }
                if nextIdentity.Canonical() != originalIdentity.Canonical() || resolutionSemantic(sessionChanged) != originalSemantic || nextBinding == originalBinding {
                    t.Fatal("durable identity/semantic and full Session binding are not separated")
                }
                changed := []mc.ResolveRequest{original.Clone()}
                changed[0].Consumer.OperationID = mustID[struct{}](t).String()
                if purpose == mc.MemoryEmbedding {
                    next := original.Clone()
                    agent := mustID[id.Agent](t)
                    next.Consumer.AgentID = &agent
                    changed = append(changed, next)
                }
                for _, next := range changed {
                    if err := next.Validate(); err != nil { t.Fatal("semantic negative must remain structurally legal", err) }
                    nextIdentity, err := resolutionIdentity(next)
                    if err != nil { t.Fatal(err) }
                    if nextIdentity.Canonical() != originalIdentity.Canonical() || resolutionSemantic(next) == originalSemantic {
                        t.Fatal("same Project/Call changed Operation or Agent did not retain unit and change semantic")
                    }
                }
                otherProject := original.Clone()
                otherProject.Consumer.ProjectID = mustID[id.Project](t)
                if err := otherProject.Validate(); err != nil { t.Fatal(err) }
                otherIdentity, err := resolutionIdentity(otherProject)
                if err != nil || otherIdentity.Canonical() == originalIdentity.Canonical() { t.Fatal("another Project shares resolution unit", err) }
            })
        }
    })
    t.Run("public_valid_empty_representations", func(t *testing.T) {
        selection := mc.SelectionRef{Kind: "platform", Selector: mc.EmbeddingSelector}
        positives := 0
        for _, raw := range []string{"{}", " { } ", "{\n\t}"} {
            for _, explicitEmpty := range []bool{false, true} {
                length := mc.TokenCount(16384)
                publicProvider := mc.ProviderInput{Name: "independent-embedding", Protocol: mc.OpenAIEmbeddings, BaseURL: "https://embedding.invalid/v1", Enabled: true, Options: json.RawMessage(raw)}
                publicModel := mc.ModelInput{Name: "independent-float", ProviderModelID: "independent-model", Type: mc.EmbeddingModel, Enabled: true, Parameters: json.RawMessage(raw), RequestOverwrite: json.RawMessage(raw), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}, ContextLength: &length}}
                if explicitEmpty {
                    publicModel.HeaderOverwrite = map[string]string{}
                    publicModel.Capabilities.ReasoningEfforts = []string{}
                    publicModel.Capabilities.StructuredOutputModes = []string{}
                }
                if publicProvider.Validate() != nil || publicModel.Validate() != nil { t.Fatal("positive representation not public-contract legal") }
                p := providerRecord{Input: providerInput{Name: publicProvider.Name, Protocol: publicProvider.Protocol, BaseURL: publicProvider.BaseURL, Enabled: publicProvider.Enabled, Options: publicProvider.Options}}
                m := modelRecord{Input: publicModel}
                before, err := json.Marshal([]any{p.Input, m.Input})
                if err != nil { t.Fatal(err) }
                revision, err := resolutionProfile(&p, &m, selection)
                after, marshalErr := json.Marshal([]any{p.Input, m.Input})
                if err != nil || marshalErr != nil || revision != adapter.OpenAIEmbeddingsFloatRevision || p.Input.Protocol.Profile() != mc.OpenAIEmbeddingsV1 { t.Fatal("legal empty representation rejected", err, marshalErr) }
                if !bytes.Equal(before, after) || string(p.Input.Options) != raw || string(m.Input.Parameters) != raw || string(m.Input.RequestOverwrite) != raw || *m.Input.Capabilities.ContextLength != 16384 {
                    t.Fatal("profile check normalized or mutated caller-owned data")
                }
                positives++
            }
        }
        negatives := 0
        for _, field := range []string{"options", "parameters", "request_overwrite"} {
            publicProvider := mc.ProviderInput{Name: "independent-embedding", Protocol: mc.OpenAIEmbeddings, BaseURL: "https://embedding.invalid/v1", Enabled: true, Options: json.RawMessage("{}")}
            publicModel := mc.ModelInput{Name: "independent-float", ProviderModelID: "independent-model", Type: mc.EmbeddingModel, Enabled: true, Parameters: json.RawMessage("{}"), RequestOverwrite: json.RawMessage("{}"), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}}}
            nonempty := json.RawMessage("{\"unverified\":null}")
            switch field { case "options": publicProvider.Options = nonempty; case "parameters": publicModel.Parameters = nonempty; case "request_overwrite": publicModel.RequestOverwrite = nonempty }
            if publicProvider.Validate() != nil || publicModel.Validate() != nil { t.Fatal("negative representation must be public-contract legal", field) }
            p := providerRecord{Input: providerInput{Name: publicProvider.Name, Protocol: publicProvider.Protocol, BaseURL: publicProvider.BaseURL, Enabled: publicProvider.Enabled, Options: publicProvider.Options}}
            m := modelRecord{Input: publicModel}
            before, err := json.Marshal([]any{p.Input, m.Input})
            if err != nil { t.Fatal(err) }
            revision, err := resolutionProfile(&p, &m, selection)
            var fault *f.Fault
            after, marshalErr := json.Marshal([]any{p.Input, m.Input})
            if !errors.As(err, &fault) || fault.Code != f.CapabilityUnsupported || revision != "" || marshalErr != nil || !bytes.Equal(before, after) { t.Fatal("null-valued nonempty config accepted or stripped", field, err) }
            negatives++
        }
        t.Logf("public-valid representations: positive=%d rejected_nonempty=%d", positives, negatives)
    })
    t.Run("no_ref_never_queries", func(t *testing.T) {
        for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
            req := independentEmbeddingRequest(t, purpose, mustID[id.User](t))
            // A nil executor panics if any SQL method is reached. This tests
            // only the private no-ref early return, not durable authority.
            lease, err := canonicalResolutionLease(context.Background(), nil, req, resolutionDraft{})
            if err != nil || lease != "" { t.Fatal("no-ref queried or invented lease", purpose, lease, err) }
        }
    })
}
