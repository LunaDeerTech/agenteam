package memory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

type RuntimeConfig struct {
	Limits rc.Limits
}
type Runtime struct {
	domain *Service
	models *Models
	search rc.Searcher
	config RuntimeConfig
}

func NewRuntime(domain *Service, models *Models, search rc.Searcher, config RuntimeConfig) (*Runtime, error) {
	if domain == nil || models == nil || models.domain != domain || nilPort(search) {
		return nil, fault(f.DependencyUnbound)
	}
	if config.Limits.Validate() != nil || config.Limits.ResultK > c.MaxRecall {
		return nil, fault(f.InvalidArgument)
	}
	return &Runtime{domain, models, search, config}, nil
}
func (r *Runtime) RequireBuiltinBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	return r.domain.RequireBuiltinBindingInTx(ctx, tx, store)
}
func (r *Runtime) StopAdmission()                  { r.domain.StopAdmission() }
func (r *Runtime) Drain(ctx context.Context) error { return r.models.Drain(ctx) }
func (r *Runtime) Joined() bool                    { return r == nil || r.models.Joined() }

func (r *Runtime) JoinOperation(ctx context.Context, actor i.Actor, namespace c.Namespace, operation tc.OperationID) error {
	if r == nil || actor.Validate() != nil || namespace.Validate() != nil || operation.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	r.models.mu.Lock()
	pending := []*modelPending{}
	for p := range r.models.pending {
		entry := p.inv.entry
		if entry.actor.Equal(actor) && entry.namespace == namespace && entry.operation == operation {
			if _, finished := r.models.finished[entry]; !finished {
				r.models.mu.Unlock()
				return fault(f.ResourceBusy)
			}
			pending = append(pending, p)
		}
	}
	builds := []*retainedGeneration{}
	for g := range r.models.builds {
		if g.entry.actor.Equal(actor) && g.entry.namespace == namespace && g.entry.operation == operation {
			if _, finished := r.models.finished[g.entry]; !finished {
				r.models.mu.Unlock()
				return fault(f.ResourceBusy)
			}
			builds = append(builds, g)
		}
	}
	r.models.mu.Unlock()
	var result error
	for _, p := range pending {
		entry := p.inv.entry
		if _, err := r.domain.deps.Tools.DiscoverMemoryTool(ctx, actor, namespace.ProjectID, namespace.AgentID, operation, entry.intent); err != nil {
			return portError(err)
		}
		result = errors.Join(result, r.models.close(ctx, p))
	}
	for _, g := range builds {
		if _, err := r.domain.deps.Tools.DiscoverMemoryTool(ctx, actor, namespace.ProjectID, namespace.AgentID, operation, g.entry.intent); err != nil {
			return portError(err)
		}
		result = errors.Join(result, g.cleanup(ctx))
	}
	return result
}

func (r *Runtime) Retain(ctx context.Context, actor i.Actor, namespace c.Namespace, operation tc.OperationID, input c.RetainInput) (c.RetainResult, error) {
	if input.Validate() != nil {
		return c.RetainResult{}, fault(f.InvalidArgument)
	}
	run, entry, err := r.domain.begin(ctx, actor, namespace, operation, "retain")
	if err != nil {
		return c.RetainResult{}, err
	}
	defer r.models.finish(entry)
	raw, err := encode(input)
	if err != nil {
		return c.RetainResult{}, err
	}
	checked, err := inspect(run, r.domain.deps.Gate, string(raw))
	if err != nil {
		return c.RetainResult{}, err
	}
	var sanitized c.RetainInput
	if c.Decode([]byte(checked.Text), &sanitized, 48<<10) != nil || sanitized.Validate() != nil {
		return c.RetainResult{}, fault(f.Forbidden)
	}
	result, found, err := r.domain.prepareRetain(run, entry, digest(raw), checked.Outcome, r.models.authority)
	if err != nil || found {
		return result, err
	}
	text, extractionCall, err := r.models.chat(run, entry, mc.MemoryExtraction, messages(extractionPrompt, checked.Text), extractionFormat())
	if err != nil {
		return c.RetainResult{}, err
	}
	var extracted c.Extraction
	if c.Decode([]byte(text), &extracted, 256<<10) != nil || extracted.Validate() != nil {
		return c.RetainResult{}, fault(f.DependencyUnavailable)
	}
	for _, candidate := range extracted.Candidates {
		payload, err := encode(candidate)
		if err != nil {
			return c.RetainResult{}, err
		}
		checked, err := inspect(run, r.domain.deps.Gate, string(payload))
		if err != nil {
			return c.RetainResult{}, err
		}
		if checked.Text != string(payload) {
			return c.RetainResult{}, fault(f.Forbidden)
		}
	}
	for attempt := 1; attempt <= 2; attempt++ {
		proposal := c.Proposal{Actions: []c.Action{}}
		related := []c.Revision{}
		proposalCall := extractionCall
		if len(extracted.Candidates) > 0 {
			var query strings.Builder
			for _, candidate := range extracted.Candidates {
				query.WriteString(candidate.Text)
				query.WriteByte('\n')
			}
			queryText := query.String()
			if len(queryText) > c.MaxQueryBytes {
				queryText = queryText[:c.MaxQueryBytes]
				for !utf8.ValidString(queryText) {
					queryText = queryText[:len(queryText)-1]
				}
			}
			recalled, err := r.recall(run, entry, c.RecallInput{Query: queryText, Limit: c.MaxRelated})
			if err != nil {
				return c.RetainResult{}, err
			}
			related = recalled.Memories
			payload, err := encode(struct {
				Candidates []c.Content  `json:"candidates"`
				Existing   []c.Revision `json:"existing_memories"`
			}{extracted.Candidates, related})
			if err != nil {
				return c.RetainResult{}, err
			}
			var text string
			text, proposalCall, err = r.models.chat(run, entry, mc.MemoryConsolidation, messages(consolidationPrompt, string(payload)), consolidationFormat())
			if err != nil {
				return c.RetainResult{}, err
			}
			if c.Decode([]byte(text), &proposal, 512<<10) != nil || proposal.Validate() != nil {
				return c.RetainResult{}, fault(f.DependencyUnavailable)
			}
			checked, err := inspect(run, r.domain.deps.Gate, text)
			if err != nil {
				return c.RetainResult{}, err
			}
			if checked.Text != text {
				return c.RetainResult{}, fault(f.Forbidden)
			}
		}
		if err := r.domain.saveProposal(run, entry, attempt, proposal, related, proposalCall); err != nil {
			return c.RetainResult{}, err
		}
		result, err = r.domain.applyProposal(run, entry, proposal, related, attempt)
		if err == nil {
			// Canonical success is durable even if provider availability delays
			// index projection. The receipt names that outcome explicitly.
			result.Indexing = "ready"
			if err := r.ensureIndex(run, entry); err != nil {
				result.Indexing = "pending"
			}
			return result, nil
		}
		var conflict *f.Fault
		if !errors.As(err, &conflict) || conflict.Code != f.VersionConflict || conflict.CommitState == f.Unknown || attempt == 2 {
			return c.RetainResult{}, err
		}
	}
	return c.RetainResult{}, fault(f.InternalError)
}

func (r *Runtime) LookupRetain(ctx context.Context, actor i.Actor, namespace c.Namespace, operation tc.OperationID, input c.RetainInput) (c.RetainResult, bool, error) {
	if input.Validate() != nil {
		return c.RetainResult{}, false, fault(f.InvalidArgument)
	}
	run, entry, err := r.domain.begin(ctx, actor, namespace, operation, "lookup-retain")
	if err != nil {
		return c.RetainResult{}, false, err
	}
	defer r.domain.end(entry)
	raw, err := encode(input)
	if err != nil {
		return c.RetainResult{}, false, err
	}
	cause, err := readCause("retain-lookup")
	if err != nil {
		return c.RetainResult{}, false, err
	}
	var result c.RetainResult
	found := false
	err = r.domain.transaction(run, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		var stored, execution, phase string
		var receipt []byte
		err := x.QueryRow(ctx, `SELECT input_digest,execution_id::text,phase,receipt FROM agenteam_memory.operations WHERE operation_id=$1 AND project_id=$2 AND agent_id=$3`, operation.String(), namespace.ProjectID.String(), namespace.AgentID.String()).Scan(&stored, &execution, &phase, &receipt)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return portError(err)
		}
		if stored != string(digest(raw)) || execution != actor.Details().ExecutionID {
			return fault(f.IdempotencyKeyReused)
		}
		if phase != "committed" {
			return nil
		}
		if json.Unmarshal(receipt, &result) != nil {
			return fault(f.DependencyUnavailable)
		}
		result.Indexing = "pending"
		var pending bool
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_memory.index_intents WHERE project_id=$1 AND agent_id=$2)`, namespace.ProjectID.String(), namespace.AgentID.String()).Scan(&pending); err != nil {
			return portError(err)
		}
		if !pending {
			result.Indexing = "ready"
		}
		found = true
		return nil
	})
	return result, found, err
}

func (r *Runtime) Recall(ctx context.Context, actor i.Actor, namespace c.Namespace, operation tc.OperationID, input c.RecallInput) (c.RecallResult, error) {
	if input.Validate() != nil {
		return c.RecallResult{}, fault(f.InvalidArgument)
	}
	run, entry, err := r.domain.begin(ctx, actor, namespace, operation, "recall")
	if err != nil {
		return c.RecallResult{}, err
	}
	defer r.models.finish(entry)
	return r.recall(run, entry, input)
}
func (r *Runtime) recall(ctx context.Context, entry *call, input c.RecallInput) (c.RecallResult, error) {
	checked, err := inspect(ctx, r.domain.deps.Gate, input.Query)
	if err != nil {
		return c.RecallResult{}, err
	}
	// Cleanup also applies when the last canonical memory was deleted while
	// the old process owned an unpublished generation.
	if err := r.ensureIndex(ctx, entry); err != nil {
		return c.RecallResult{}, err
	}
	active, err := r.domain.hasActive(ctx, entry)
	if err != nil {
		return c.RecallResult{}, err
	}
	if !active {
		return c.RecallResult{Memories: []c.Revision{}, Mode: "empty"}, nil
	}
	serving, exists, err := r.domain.serving(ctx, entry)
	if err != nil {
		return c.RecallResult{}, err
	}
	if !exists {
		return c.RecallResult{}, fault(f.DependencyUnavailable)
	}
	limits := r.config.Limits
	if input.Limit > 0 {
		limits.ResultK = input.Limit
		if limits.ResultK > limits.FusedK {
			return c.RecallResult{}, fault(f.InvalidArgument)
		}
	}
	agent := entry.namespace.AgentID
	request := rc.Request{Query: checked.Text, Scope: rc.Scope{Kind: rc.Memory, ProjectID: entry.namespace.ProjectID, AgentID: &agent}, Serving: serving, Limits: limits, Filters: rc.Filters{MemoryTypes: filterTypes(input.Filters), Tags: append([]string{}, input.Filters.Tags...)}}
	result, err := r.search.Search(ctx, request)
	if err != nil {
		return c.RecallResult{}, err
	}
	return r.domain.projectRecall(ctx, entry, serving, result)
}
func (r *Runtime) Reflect(ctx context.Context, actor i.Actor, namespace c.Namespace, operation tc.OperationID, input c.ReflectInput) (c.ReflectResult, error) {
	if input.Validate() != nil {
		return c.ReflectResult{}, fault(f.InvalidArgument)
	}
	run, entry, err := r.domain.begin(ctx, actor, namespace, operation, "reflect")
	if err != nil {
		return c.ReflectResult{}, err
	}
	defer r.models.finish(entry)
	checked, err := inspect(run, r.domain.deps.Gate, input.Query)
	if err != nil {
		return c.ReflectResult{}, err
	}
	input.Query = checked.Text
	recalled, err := r.recall(run, entry, c.RecallInput{Query: input.Query})
	if err != nil {
		return c.ReflectResult{}, err
	}
	refs := make([]c.Target, len(recalled.Memories))
	for n, v := range recalled.Memories {
		refs[n] = c.Target{MemoryID: v.MemoryID, Revision: v.Revision}
	}
	if len(refs) == 0 {
		return c.ReflectResult{Answer: "No matching active memories were found.", MemoryRefs: refs}, nil
	}
	payload, err := encode(struct {
		Query    string       `json:"query"`
		Memories []c.Revision `json:"memories"`
	}{input.Query, recalled.Memories})
	if err != nil {
		return c.ReflectResult{}, err
	}
	answer, _, err := r.models.chat(run, entry, mc.MemoryReflection, messages(reflectionPrompt, string(payload)), mc.ResponseFormat{Kind: "text"})
	if err != nil {
		return c.ReflectResult{}, err
	}
	cause, err := readCause("reflect-current")
	if err != nil {
		return c.ReflectResult{}, err
	}
	err = r.domain.transaction(run, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		for _, ref := range refs {
			record, err := readRecord(ctx, x, namespace, ref.MemoryID)
			if err != nil {
				return err
			}
			if record == nil || record.Current.Status != c.Active || record.Current.Revision != ref.Revision {
				return fault(f.VersionConflict)
			}
		}
		return nil
	})
	if err != nil {
		return c.ReflectResult{}, err
	}
	return c.ReflectResult{Answer: answer, MemoryRefs: refs}, nil
}

// A profile change builds a complete replacement before the atomic switch.
// Otherwise only missing revisions are projected into the pinned space.
func (r *Runtime) ensureIndex(ctx context.Context, entry *call) error {
	if err := r.models.recoverGenerations(ctx, entry); err != nil {
		return err
	}
	active, err := r.domain.hasActive(ctx, entry)
	if err != nil || !active {
		return err
	}
	profile, err := r.models.currentProfile(ctx, entry)
	if err != nil {
		return err
	}
	serving, exists, err := r.domain.serving(ctx, entry)
	if err != nil {
		return err
	}
	if exists {
		matched, err := r.domain.servingProfileMatches(ctx, entry, serving, profile)
		if err != nil {
			return err
		}
		if matched {
			return r.updateIndex(ctx, entry, serving)
		}
	}
	source, err := r.domain.captureGeneration(ctx, entry)
	if err != nil {
		return err
	}
	batchLimit := 262144 / profile.Dimensions
	if batchLimit > 128 {
		batchLimit = 128
	}
	if len(source.Records) > batchLimit {
		return fault(f.PayloadTooLarge)
	}
	texts := make([]string, len(source.Records))
	for n, record := range source.Records {
		texts[n] = record.Current.Text
	}
	g, err := r.models.newGeneration(ctx, entry, source, profile)
	if err != nil {
		return err
	}
	// Failed/cancelled cleanup remains registered for original Tool Join/Drain.
	defer func() { _ = g.finish(ctx) }()
	response, _, err := r.models.embedWithGeneration(ctx, entry, texts, profile.Dimensions, nil, g)
	if err != nil {
		return err
	}
	vectors := make([]generationVector, len(texts))
	for _, item := range response.Items {
		record := source.Records[item.Index]
		vectors[item.Index] = generationVector{Target: c.Target{MemoryID: record.Current.MemoryID, Revision: record.Current.Revision}, Vector: item.Values}
	}
	_, err = g.publish(ctx, vectors)
	return err
}

var _ MemoryTools = (*Runtime)(nil)
