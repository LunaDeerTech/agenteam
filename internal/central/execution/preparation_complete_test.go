package execution

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Domain results and physical outcomes are explicit controls. These tests use
// the real preparation driver, Model consumer, input codec/repository and
// original Tx gates; real provider storage and migration 54 need PostgreSQL.
type completeCaptureControl struct {
	*resourceCaptureControl
	consumer     mc.ConsumerRequest
	consumerPlan mc.ConsumerDependencies
	resolved     mc.ResolvedModel
	modelCtx     context.Context
	modelScope   mc.ExecutionModelCaptureScope
	unknown      error
	observation  mc.ModelCaptureObservation
	observations int
	changeModel  bool
}

func newCompleteCaptureControl(t *testing.T) *completeCaptureControl {
	r := &completeCaptureControl{resourceCaptureControl: newResourceCaptureControl(t)}
	p := r.p
	actor, _ := i.NewAgentRun(p.project.ID, p.launch.request.AgentID, p.execution)
	owner, err := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, p.execution.String())
	if err != nil {
		t.Fatal(err)
	}
	consumer := mc.Consumer{Kind: mc.AgentConsumer, ProjectID: p.project.ID, AgentID: &p.launch.request.AgentID, ExecutionID: &p.execution, Purpose: mc.AgentGeneration}
	model := p.launch.config.Fields().Core.ModelRef
	resolve := mc.ResolveRequest{Actor: actor, Consumer: consumer, Purpose: mc.AgentGeneration, Source: mc.CurrentSelectionSource, ModelRef: &model, Selection: &mc.SelectionRef{Kind: "direct"}, LeaseOwner: owner}
	snapshot := mc.ConfigSnapshot{ID: newTestID[mc.Snapshot](t), Identity: mc.ModelIdentity{ProviderID: newTestID[mc.Provider](t), ModelID: model, ProviderName: "provider", ModelName: "chat", ProviderModelID: "upstream", Protocol: mc.OpenAIChat, Profile: mc.OpenAIChatV1, ModelType: mc.ChatModel, AdapterRevision: "native-v1"}, Endpoint: "https://provider.example/v1", Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}
	r.resolved = mc.ResolvedModel{Snapshot: snapshot, Consumer: consumer, LeaseOwner: owner}
	r.consumer = mc.ConsumerRequest{Action: mc.ResolveConsumer, Actor: actor, Consumer: consumer, Resolve: &resolve, SnapshotID: snapshot.ID, LeaseOwner: owner}
	if r.resolved.Validate() != nil || r.consumer.Validate() != nil {
		t.Fatal("invalid controlled Model data")
	}
	p.driver.state.models, p.driver.state.environment, p.driver.state.mounts = r, r, r
	return r
}

func (r *completeCaptureControl) DiscoverExecutionModel(ctx context.Context, request mc.ExecutionModelCaptureRequest) (mc.ExecutionModelCapturePlan, error) {
	r.modelCtx = ctx
	a, t := r.p.launch.authority, r.p.launch.t
	_, err := a.Discover(ctx, r.consumer)
	requireCode(t, err, f.Forbidden) // A bare planning carrier has not read its source.
	plan, err := r.discovery(ctx, "model", func(ctx context.Context, tx f.Tx) (f.Digest, error) {
		_, e := a.RequireModelCaptureInTx(ctx, tx, request)
		requireCode(t, e, f.Forbidden)
		scope, e := a.RequireModelCaptureDiscoveryInTx(ctx, tx, request)
		r.modelScope = scope.Clone()
		return scope.AttemptBinding, e
	})
	if err != nil {
		return nil, err
	}
	r.consumerPlan, err = a.Discover(ctx, r.consumer)
	if err != nil {
		return nil, err
	}
	_, err = r.discovery(ctx, "model-validate", func(ctx context.Context, tx f.Tx) (f.Digest, error) {
		if e := a.ValidateInTx(ctx, tx, r.consumer, r.consumerPlan); e != nil {
			return "", e
		}
		return plan.binding, nil
	})
	if err != nil {
		return nil, err
	}
	if r.unknown != nil {
		return nil, r.unknown
	}
	if r.changeModel {
		fields := r.p.launch.config.Fields()
		fields.Core.ModelRef = newTestID[mc.Model](t)
		r.p.launch.config, err = ac.NewAgentConfig(fields)
		if err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func (r *completeCaptureControl) ResolveExecutionModelInTx(ctx context.Context, tx f.Tx, request mc.ExecutionModelCaptureRequest, plan mc.ExecutionModelCapturePlan) (mc.ResolvedModel, error) {
	a, t := r.p.launch.authority, r.p.launch.t
	facts, err := a.RequireModelCaptureInTx(ctx, tx, request)
	if err != nil {
		return mc.ResolvedModel{}, err
	}
	if facts.AgentVersion != r.p.launch.config.Fields().Core.Version {
		t.Fatal("Model source did not capture actual Agent version")
	}
	if err = r.requirePlan(ctx, tx, plan, "model", facts.Scope.AttemptBinding); err != nil {
		return mc.ResolvedModel{}, err
	}
	_, err = a.Discover(r.modelCtx, r.consumer)
	requireCode(t, err, f.Forbidden)
	other := r.consumer.Clone()
	other.SnapshotID = newTestID[mc.Snapshot](t)
	requireCode(t, a.ValidateInTx(ctx, tx, other, r.consumerPlan), f.ConfirmationStale)
	foreign, err := mc.NewConsumerDependencies(mc.NewPlanIssuer(), r.consumerPlan.Details())
	if err != nil {
		t.Fatal(err)
	}
	requireCode(t, a.ValidateInTx(ctx, tx, r.consumer, foreign), f.ConfirmationStale)
	requireCode(t, a.ValidateInTx(context.Background(), tx, r.consumer, r.consumerPlan), f.Forbidden)
	if err = a.ValidateInTx(ctx, tx, r.consumer, r.consumerPlan); err != nil {
		return mc.ResolvedModel{}, err
	}
	return r.resolved.Clone(), nil
}

func (r *completeCaptureControl) DiscoverExecutionEnvironment(ctx context.Context, request pv.EnvironmentCaptureRequest) (pv.EnvironmentCapturePlan, error) {
	return r.discovery(ctx, "environment", func(ctx context.Context, tx f.Tx) (f.Digest, error) {
		facts, err := r.p.launch.authority.RequireEnvironmentDiscoveryInTx(ctx, tx, request)
		if err == nil {
			err = facts.Validate(request)
		}
		return facts.AttemptBinding, err
	})
}
func (r *completeCaptureControl) ResolveExecutionEnvironmentInTx(ctx context.Context, tx f.Tx, request pv.EnvironmentCaptureRequest, plan pv.EnvironmentCapturePlan) (pv.EnvironmentCapture, error) {
	facts, err := r.p.launch.authority.RequireEnvironmentCaptureInTx(ctx, tx, request)
	if err != nil {
		return pv.EnvironmentCapture{}, err
	}
	if err = facts.Validate(request); err != nil {
		return pv.EnvironmentCapture{}, err
	}
	if err = r.requirePlan(ctx, tx, plan, "environment", facts.AttemptBinding); err != nil {
		return pv.EnvironmentCapture{}, err
	}
	return pv.NewEnvironmentCapture(pv.EnvironmentCaptureFields{Request: request, AttemptBinding: facts.AttemptBinding, AgentVersion: facts.AgentVersion, Variables: []pv.Variable{}, Secrets: []pv.ExecutionSecretVariable{}})
}
func (r *completeCaptureControl) DiscoverExecutionMounts(ctx context.Context, request mt.ExecutionMountCaptureRequest) (mt.ExecutionMountCapturePlan, error) {
	return r.discovery(ctx, "mount", func(ctx context.Context, tx f.Tx) (f.Digest, error) {
		facts, err := r.p.launch.authority.RequireMountCaptureDiscoveryInTx(ctx, tx, request)
		return facts.AttemptBinding, err
	})
}
func (r *completeCaptureControl) CaptureExecutionMountsInTx(ctx context.Context, tx f.Tx, request mt.ExecutionMountCaptureRequest, plan mt.ExecutionMountCapturePlan) (mt.ExecutionMountSet, error) {
	facts, err := r.p.launch.authority.RequireMountCaptureInTx(ctx, tx, request)
	if err != nil {
		return mt.ExecutionMountSet{}, err
	}
	if len(facts.AllowedMountIDs) != 0 {
		return mt.ExecutionMountSet{}, fault(f.DependencyUnbound)
	}
	if err = r.requirePlan(ctx, tx, plan, "mount", facts.Scope.AttemptBinding); err != nil {
		return mt.ExecutionMountSet{}, err
	}
	return mt.ExecutionMountSet{Request: request, AgentVersion: facts.AgentVersion, Mounts: []mt.ExecutionMountMetadata{}}, nil
}

type controlledModelUnknown struct{}

func (*controlledModelUnknown) Error() string { return "controlled-model-unknown" }
func (*controlledModelUnknown) Unwrap() error { return f.NewFault(f.CommitUnknown, f.Unknown) }
func (r *completeCaptureControl) ObserveExecutionModelDiscovery(ctx context.Context, request mc.ExecutionModelCaptureRequest, original error) (mc.ModelCaptureObservation, error) {
	r.observations++
	if original != r.unknown {
		return "", fault(f.Forbidden)
	}
	_, err := r.discovery(ctx, "model-observe", func(ctx context.Context, tx f.Tx) (f.Digest, error) {
		facts, err := r.p.launch.authority.RequireModelCaptureDiscoveryInTx(ctx, tx, request)
		if err != nil {
			return "", err
		}
		if !reflect.DeepEqual(facts, r.modelScope) {
			r.p.launch.t.Fatal("recovery lost original Model scope")
		}
		_, err = r.p.launch.authority.Discover(ctx, r.consumer)
		requireCode(r.p.launch.t, err, f.Forbidden)
		requireCode(r.p.launch.t, r.p.launch.authority.ValidateInTx(ctx, tx, r.consumer, r.consumerPlan), f.Forbidden)
		return facts.AttemptBinding, nil
	})
	return r.observation, err
}

func TestExecutionPreparationCompleteInputAndConsumerAuthority(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "changed-model"}[changed], func(t *testing.T) {
			r := newCompleteCaptureControl(t)
			p := r.p
			r.changeModel = changed
			err := p.driver.Run(context.Background(), p.execution)
			if changed {
				requireCode(t, err, f.ConfirmationStale)
				if len(p.store.inputs) != 0 || p.store.providerRefs != 0 {
					t.Fatal("changed model retained partial capture")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			stored, err := scanPreparationInput(launchScan{values: p.store.inputs[p.execution.String()]})
			if err != nil || stored == nil {
				t.Fatal("complete input did not round trip", err)
			}
			if p.store.rows[p.execution.String()][3] != "preparing" || p.store.rows[p.execution.String()][6] != (*string)(nil) {
				t.Fatal("capture advanced into Snapshot/runtime")
			}
			if p.store.providerRefs != 6 || len(p.store.attempts) != 1 {
				t.Fatal("provider writes did not commit together")
			}
			calls := len(r.order)
			if err = p.driver.Run(context.Background(), p.execution); err != nil || len(r.order) != calls || len(p.store.attempts) != 1 {
				t.Fatal("input replay recaptured sources", err)
			}
			retire := r.consumer.Clone()
			retire.Action = mc.RetireConsumer
			retire.Resolve = nil
			lease := newTestID[sc.Lease](t)
			retire.LeaseID = &lease
			_, err = p.launch.authority.Discover(context.Background(), retire)
			requireCode(t, err, f.DependencyUnbound)
		})
	}
}

func TestExecutionPreparationInputUnknownAndAtomicRollback(t *testing.T) {
	t.Run("original-input-observation", func(t *testing.T) {
		r := newCompleteCaptureControl(t)
		p := r.p
		p.store.unknownAt = "capture"
		original := p.driver.Run(context.Background(), p.execution)
		if _, ok := UnknownAttempt(original); !ok {
			t.Fatal("lost original capture commit", original)
		}
		p.driver.Stop()
		if p.driver.Joined() {
			t.Fatal("unknown capture joined")
		}
		row := p.store.inputs[p.execution.String()]
		delete(p.store.inputs, p.execution.String())
		calls := len(r.order)
		if err := p.driver.ResolveUnknown(context.Background(), p.execution); err != original {
			t.Fatal("absent input resolved original unknown", err)
		}
		p.store.inputs[p.execution.String()] = row
		if err := p.driver.ResolveUnknown(context.Background(), p.execution); err != nil || !p.driver.Joined() || len(r.order) != calls {
			t.Fatal("original input observation recaptured or failed to join", err)
		}
	})
	t.Run("missing-provider", func(t *testing.T) {
		r := newCompleteCaptureControl(t)
		p := r.p
		p.driver.state.mounts = nil
		requireCode(t, p.driver.Run(context.Background(), p.execution), f.DependencyUnbound)
		if len(p.store.inputs) != 0 || p.store.providerRefs != 0 {
			t.Fatal("missing provider committed partial input/references")
		}
	})
}

func TestExecutionPreparationModelUnknownRecoveryUsesOriginalScope(t *testing.T) {
	for _, observation := range []mc.ModelCaptureObservation{mc.ModelCaptureReadOnly, mc.ModelCapturePrepared} {
		t.Run(string(observation), func(t *testing.T) {
			r := newCompleteCaptureControl(t)
			p := r.p
			r.unknown = &controlledModelUnknown{}
			original := p.driver.Run(context.Background(), p.execution)
			if original != r.unknown {
				t.Fatal("foreign original Unknown was replaced", original)
			}
			p.driver.Stop()
			if p.driver.Joined() {
				t.Fatal("foreign Unknown lost owner")
			}
			r.observation = mc.ModelCapturePending
			if err := p.driver.ResolveUnknown(context.Background(), p.execution); err != original {
				t.Fatal("absent Model intent retired ownership", err)
			}
			// Cancellation and later Project facts cannot erase the original Model
			// transaction. Its recovery carrier cannot mint another Consumer plan.
			at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
			p.store.rows[p.execution.String()][5] = &at
			p.project.Version++
			p.project.Lifecycle = pc.Archived
			r.observation = observation
			if err := p.driver.ResolveUnknown(context.Background(), p.execution); err != nil || !p.driver.Joined() {
				t.Fatal("original Model observation failed to join", err)
			}
			if p.captures != 0 || p.launch.captures != 0 || len(p.store.inputs) != 0 || len(p.store.attempts) != 1 || r.observations != 2 {
				t.Fatal("observation entered capture/new attempt")
			}
			if !errors.Is(original, r.unknown) {
				t.Fatal("original error identity lost")
			}
		})
	}
}
