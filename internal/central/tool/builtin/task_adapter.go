package builtin

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type TaskToolService interface {
	ReadTask(context.Context, i.Actor, wc.ProjectID, wc.TaskID) (wc.Task, error)
	TransferTask(context.Context, i.Actor, f.CommandMeta, wc.ProjectID, wc.TaskID, wc.TaskTransfer) (wc.TaskTransitionMutation, error)
	LookupTaskToolTransfer(context.Context, i.Actor, f.CommandMeta, wc.ProjectID, wc.TaskID, wc.TaskTransfer) (wc.TaskTransitionLookup, error)
}

type TaskAdapter struct{ service TaskToolService }

func NewTaskAdapter(service TaskToolService) (*TaskAdapter, error) {
	if skillInstallNil(service) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &TaskAdapter{service}, nil
}

func (a *TaskAdapter) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if a == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	service, ok := a.service.(*work.TaskTransitionService)
	if !ok || service == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return service.RequireBuiltinBindingInTx(ctx, tx, store)
}

type taskToolArguments struct {
	task     wc.TaskID
	version  f.Version
	transfer wc.TaskTransfer
}

func taskToolInvalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }

func parseTaskTool(ctx context.Context, name string, raw []byte) (taskToolArguments, error) {
	var out taskToolArguments
	if ctx == nil || len(raw) == 0 || len(raw) > wc.MaxTaskTransferBytes || !utf8.Valid(raw) || skillInstallUnicode(ctx, raw) != nil {
		return out, taskToolInvalid()
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if name == TaskReadName {
		err := artifactObject(raw, []string{"task_id"}, map[string]any{"task_id": &out.task})
		if err != nil || out.task.Validate() != nil {
			return out, taskToolInvalid()
		}
		return out, nil
	}
	if name != TaskTransferName {
		return out, taskToolInvalid()
	}
	var target, assignee, comment, adds, resolves json.RawMessage
	err := artifactObject(raw, []string{"task_id", "expected_version", "target_state"}, map[string]any{"task_id": &out.task, "expected_version": &out.version, "target_state": &target, "assignee_agent_id": &assignee, "comment": &comment, "add_blockers": &adds, "resolve_blocker_ids": &resolves})
	if err != nil || out.task.Validate() != nil || out.version.Validate() != nil {
		return out, taskToolInvalid()
	}
	fields := map[string]json.RawMessage{"target_state": target}
	for key, value := range map[string]json.RawMessage{"assignee_agent_id": assignee, "comment": comment, "add_blockers": adds, "resolve_blocker_ids": resolves} {
		if value != nil {
			fields[key] = value
		}
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return out, taskToolInvalid()
	}
	out.transfer, err = wc.DecodeTaskTransfer(encoded)
	return out, err
}

func TaskTransferKey(operation tc.OperationID) (f.IdempotencyKey, error) {
	if operation.Validate() != nil {
		return "", taskToolInvalid()
	}
	key := f.IdempotencyKey("tool.task.transfer:" + operation.String())
	return key, key.Validate()
}

func (a *TaskAdapter) Execute(ctx context.Context, b tc.ToolCallBinding, operation tc.OperationID, request f.ID[f.Request], raw []byte) ([]byte, error) {
	if a == nil || skillInstallNil(a.service) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if b.Validate() != nil || operation.Validate() != nil || request.Validate() != nil {
		return nil, taskToolInvalid()
	}
	name := TaskReadName
	if b.Binding.HandlerID == TaskTransferHandlerID {
		name = TaskTransferName
	} else if b.Binding.HandlerID != TaskReadHandlerID {
		return nil, taskToolInvalid()
	}
	args, err := parseTaskTool(ctx, name, raw)
	if err != nil {
		return nil, err
	}
	if name == TaskReadName {
		task, err := a.service.ReadTask(ctx, b.Actor, b.ProjectID, args.task)
		if err != nil {
			return nil, err
		}
		return json.Marshal(task)
	}
	key, err := TaskTransferKey(operation)
	if err != nil {
		return nil, err
	}
	meta := f.CommandMeta{RequestID: request, IdempotencyKey: key, ExpectedVersion: &args.version}
	out, err := a.service.TransferTask(ctx, b.Actor, meta, b.ProjectID, args.task, args.transfer)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

func (a *TaskAdapter) Lookup(ctx context.Context, b tc.ToolCallBinding, operation tc.OperationID, request f.ID[f.Request], raw []byte) ([]byte, bool, error) {
	if a == nil || skillInstallNil(a.service) {
		return nil, false, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if b.Validate() != nil || operation.Validate() != nil || request.Validate() != nil || b.Binding.HandlerID != TaskTransferHandlerID {
		return nil, false, taskToolInvalid()
	}
	args, err := parseTaskTool(ctx, TaskTransferName, raw)
	if err != nil {
		return nil, false, err
	}
	key, err := TaskTransferKey(operation)
	if err != nil {
		return nil, false, err
	}
	meta := f.CommandMeta{RequestID: request, IdempotencyKey: key, ExpectedVersion: &args.version}
	out, err := a.service.LookupTaskToolTransfer(ctx, b.Actor, meta, b.ProjectID, args.task, args.transfer)
	if err != nil {
		return nil, false, err
	}
	if out.Validate() != nil {
		return nil, false, f.NewFault(f.DependencyUnavailable, f.Unknown)
	}
	if out.Status != wc.LookupCommitted {
		return nil, false, nil
	}
	encoded, err := json.Marshal(out.Receipt)
	return encoded, err == nil, err
}
