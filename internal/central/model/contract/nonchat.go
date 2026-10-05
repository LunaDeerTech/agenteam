package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"mime"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type NonchatCall struct {
	Actor    id.Actor      `json:"-"`
	CallID   CallID        `json:"call_id"`
	Consumer Consumer      `json:"consumer"`
	Model    ResolvedModel `json:"model"`
	Input    InputIdentity `json:"input"`
}

func (c NonchatCall) Validate() error {
	if !actorMatches(c.Actor, c.Consumer) || c.CallID.Validate() != nil || c.Consumer.Validate() != nil || c.Model.Validate() != nil || !c.Consumer.Equal(c.Model.Consumer) || !ownerMatches(c.Model.LeaseOwner, c.Consumer, &c.CallID) || c.Input.ValidateFor(c.Consumer) != nil {
		return bad()
	}
	return nil
}
func (c NonchatCall) Clone() NonchatCall {
	c.Consumer = c.Consumer.Clone()
	c.Model = c.Model.Clone()
	c.Input = c.Input.Clone()
	return c
}

type EmbeddingRequest struct {
	NonchatCall
	Texts []string `json:"texts"`
}

func (r EmbeddingRequest) Validate() error {
	if r.NonchatCall.Validate() != nil || r.Consumer.Purpose.ModelType() != EmbeddingModel || len(r.Texts) == 0 {
		return bad()
	}
	for _, s := range r.Texts {
		if !text(s, 16<<20) {
			return bad()
		}
	}
	return nil
}
func (r EmbeddingRequest) Clone() EmbeddingRequest {
	r.NonchatCall = r.NonchatCall.Clone()
	r.Texts = append([]string(nil), r.Texts...)
	return r
}

type Embedding struct {
	Index  int       `json:"index"`
	Values []float64 `json:"values"`
}
type Embeddings struct {
	InvocationID InvocationID `json:"invocation_id"`
	Items        []Embedding  `json:"items"`
	Usage        Usage        `json:"usage"`
}

func (r Embeddings) Validate() error {
	if r.InvocationID.Validate() != nil || r.Usage.Validate() != nil || len(r.Items) == 0 {
		return bad()
	}
	seen := map[int]bool{}
	dim := len(r.Items[0].Values)
	if dim == 0 {
		return bad()
	}
	for _, v := range r.Items {
		if v.Index < 0 || v.Index >= len(r.Items) || seen[v.Index] || len(v.Values) != dim {
			return bad()
		}
		seen[v.Index] = true
		for _, x := range v.Values {
			if math.IsNaN(x) || math.IsInf(x, 0) {
				return bad()
			}
		}
	}
	return nil
}
func (r Embeddings) ValidateFor(n, dimensions int) error {
	if n < 1 || dimensions < 1 || r.Validate() != nil || len(r.Items) != n || len(r.Items[0].Values) != dimensions {
		return bad()
	}
	return nil
}
func (r Embeddings) Clone() Embeddings {
	r.Usage = r.Usage.Clone()
	r.Items = append([]Embedding(nil), r.Items...)
	for n := range r.Items {
		r.Items[n].Values = append([]float64(nil), r.Items[n].Values...)
	}
	return r
}

type RerankRequest struct {
	NonchatCall
	Query      string   `json:"query"`
	Candidates []string `json:"candidates"`
	TopN       int      `json:"top_n"`
}

func (r RerankRequest) Validate() error {
	if r.NonchatCall.Validate() != nil || r.Consumer.Purpose != Rerank || !text(r.Query, 16<<20) || r.TopN < 1 || r.TopN > len(r.Candidates) {
		return bad()
	}
	for _, s := range r.Candidates {
		if !text(s, 16<<20) {
			return bad()
		}
	}
	return nil
}
func (r RerankRequest) Clone() RerankRequest {
	r.NonchatCall = r.NonchatCall.Clone()
	r.Candidates = append([]string(nil), r.Candidates...)
	return r
}

type RankedItem struct {
	Index int     `json:"index"`
	Score float64 `json:"score"`
}
type RankedItems struct {
	InvocationID InvocationID `json:"invocation_id"`
	Items        []RankedItem `json:"items"`
	Usage        Usage        `json:"usage"`
}

func (r RankedItems) Validate() error {
	if r.InvocationID.Validate() != nil || r.Usage.Validate() != nil {
		return bad()
	}
	seen := map[int]bool{}
	for _, v := range r.Items {
		if v.Index < 0 || seen[v.Index] || math.IsNaN(v.Score) || math.IsInf(v.Score, 0) {
			return bad()
		}
		seen[v.Index] = true
	}
	return nil
}
func (r RankedItems) ValidateFor(candidates, topN int) error {
	if r.Validate() != nil || topN < 1 || topN > candidates || len(r.Items) > topN {
		return bad()
	}
	for _, v := range r.Items {
		if v.Index >= candidates {
			return bad()
		}
	}
	return nil
}
func (r RankedItems) Clone() RankedItems {
	r.Usage = r.Usage.Clone()
	r.Items = append([]RankedItem(nil), r.Items...)
	return r
}

type ImageRequest struct {
	NonchatCall
	Prompt     string          `json:"prompt"`
	Parameters json.RawMessage `json:"parameters"`
	Owner      oc.ObjectOwner  `json:"owner"`
}

func (r ImageRequest) Validate() error {
	if r.NonchatCall.Validate() != nil || r.Consumer.Purpose != ImageGeneration || !text(r.Prompt, 16<<20) || objectJSON(r.Parameters, 64<<10) != nil || r.Owner.Validate() != nil || r.Owner.Details().ProjectID != r.Consumer.ProjectID.String() {
		return bad()
	}
	return nil
}
func (r ImageRequest) Clone() ImageRequest {
	r.NonchatCall = r.NonchatCall.Clone()
	r.Parameters = bytes.Clone(r.Parameters)
	return r
}

type GeneratedFile struct {
	Ref       oc.BusinessFileRef `json:"ref"`
	MediaType string             `json:"media_type"`
	Size      TokenCount         `json:"size"`
}

func (r GeneratedFile) Validate() error {
	m, _, e := mime.ParseMediaType(r.MediaType)
	if e != nil || m != r.MediaType || r.Ref.Validate() != nil || r.Size <= 0 || int64(r.Size) > oc.MaxObjectSize {
		return bad()
	}
	return nil
}

type GeneratedFiles struct {
	InvocationID InvocationID    `json:"invocation_id"`
	Items        []GeneratedFile `json:"items"`
	Usage        Usage           `json:"usage"`
}

func (r GeneratedFiles) Validate() error {
	if r.InvocationID.Validate() != nil || r.Usage.Validate() != nil || len(r.Items) == 0 {
		return bad()
	}
	for _, v := range r.Items {
		if v.Validate() != nil {
			return bad()
		}
	}
	return nil
}
func (r GeneratedFiles) Clone() GeneratedFiles {
	r.Items = append([]GeneratedFile(nil), r.Items...)
	r.Usage = r.Usage.Clone()
	return r
}

type Nonchat interface {
	Embed(context.Context, EmbeddingRequest) (Embeddings, error)
	Rerank(context.Context, RerankRequest) (RankedItems, error)
	GenerateImage(context.Context, ImageRequest) (GeneratedFiles, error)
}

func (NonchatCall) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_nonchat_call")) }
func (NonchatCall) LogValue() slog.Value       { return slog.StringValue("model_nonchat_call") }

func (EmbeddingRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_embedding_request"))
}
func (EmbeddingRequest) LogValue() slog.Value { return slog.StringValue("model_embedding_request") }

func (Embedding) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_embedding")) }
func (Embedding) LogValue() slog.Value       { return slog.StringValue("model_embedding") }

func (Embeddings) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_embeddings")) }
func (Embeddings) LogValue() slog.Value       { return slog.StringValue("model_embeddings") }

func (RerankRequest) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_rerank_request")) }
func (RerankRequest) LogValue() slog.Value       { return slog.StringValue("model_rerank_request") }

func (RankedItem) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_ranked_item")) }
func (RankedItem) LogValue() slog.Value       { return slog.StringValue("model_ranked_item") }

func (RankedItems) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_ranked_items")) }
func (RankedItems) LogValue() slog.Value       { return slog.StringValue("model_ranked_items") }

func (ImageRequest) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_image_request")) }
func (ImageRequest) LogValue() slog.Value       { return slog.StringValue("model_image_request") }

func (GeneratedFile) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_generated_file")) }
func (GeneratedFile) LogValue() slog.Value       { return slog.StringValue("model_generated_file") }

func (GeneratedFiles) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_generated_files")) }
func (GeneratedFiles) LogValue() slog.Value       { return slog.StringValue("model_generated_files") }
