// Package meeting owns the collaboration session, never the Agent execution
// engine. Every mutation enters through its current Project authority.
package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
	"github.com/jackc/pgx/v5"
)

// ReferenceAuthority validates typed identities under the caller's declared
// lock union. An absent adapter rejects referenced content explicitly.
type ReferenceAuthority interface {
	ReferenceLocks(context.Context, i.Actor, i.ProjectID, []c.Reference) ([]f.LockRequest, error)
	RequireReferencesInTx(context.Context, f.Tx, i.Actor, i.ProjectID, []c.Reference) error
}
type Dependencies struct {
	Projects           pc.ProjectAuthority
	Agents             ac.WorkReferences
	Directory          ac.DirectoryQueries
	References         ReferenceAuthority
	Attachments        ec.ArtifactAttachmentCapture
	DecisionAuthority  *Authority
	DecisionResults    ec.MeetingResultReader
	DecisionEvents     *DecisionEvents
	DeletionAuthority  *DeletionAuthority
	Audit              au.Appender
	LifecycleEvents    *ProposalEvents
	RuntimeViews       ec.MeetingRuntimeReader
	RuntimeProjections ec.MeetingRuntimeProjectionReader
	Realtime           rc.Publisher
}
type Service struct {
	store Store
	deps  Dependencies
}

func NewService(store Store, deps Dependencies) (*Service, error) {
	if nilPort(store) || nilPort(deps.Projects) || nilPort(deps.Agents) || nilPort(deps.Directory) {
		return nil, fault(f.DependencyUnbound)
	}
	return &Service{store: store, deps: deps}, nil
}
func (s *Service) owner(ctx context.Context, tx f.Tx, a i.Actor, p i.ProjectID, intent i.AccessIntent) error {
	if err := human(a); err != nil {
		return err
	}
	grant, err := s.deps.Projects.RequireOwnerInTx(ctx, tx, a, p, intent)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(a, p) {
		return fault(f.Forbidden)
	}
	return nil
}
func commandIdentity(p i.ProjectID, key f.IdempotencyKey) (f.CommandIdentity, error) {
	return f.NewCommandIdentity("meeting", []string{p.String()}, "write", key)
}
func commandDigest(a i.Actor, kind string, input any) (f.Digest, error) {
	raw, err := canonical(struct {
		Actor string
		Kind  string
		Input any
	}{a.Details().UserID, kind, input})
	if err != nil {
		return "", err
	}
	return digest(raw), nil
}
func lookupCommand(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, a i.Actor, key f.IdempotencyKey, d f.Digest) (c.Receipt, bool, error) {
	var actor, stored string
	var raw []byte
	err := x.QueryRow(ctx, `SELECT actor_id,semantic_digest,receipt FROM agenteam_meeting.commands WHERE project_id=$1 AND idempotency_key=$2`, p.String(), key.String()).Scan(&actor, &stored, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.Receipt{}, false, nil
	}
	if err != nil {
		return c.Receipt{}, false, portError(err)
	}
	if actor != a.Details().UserID || stored != d.String() {
		return c.Receipt{}, false, fault(f.IdempotencyKeyReused)
	}
	var out c.Receipt
	if json.Unmarshal(raw, &out) != nil {
		return c.Receipt{}, false, fault(f.InternalError)
	}
	out.Replayed = true
	return out, true, nil
}
func saveCommand(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, a i.Actor, key f.IdempotencyKey, kind string, d f.Digest, r c.Receipt) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return portError(err)
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.commands(project_id,idempotency_key,actor_id,command,semantic_digest,meeting_id,receipt,committed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.String(), key.String(), a.Details().UserID, kind, d.String(), r.MeetingID.String(), raw, now())
	return portError(err)
}
func references(nodes []c.InlineNode) []c.Reference {
	out := []c.Reference{}
	for _, n := range nodes {
		if n.Reference != nil {
			out = append(out, *n.Reference)
		}
	}
	return out
}
func (s *Service) referenceLocks(ctx context.Context, a i.Actor, p i.ProjectID, nodes []c.InlineNode) ([]f.LockRequest, error) {
	refs := references(nodes)
	if len(refs) == 0 {
		return nil, nil
	}
	if nilPort(s.deps.References) {
		return nil, fault(f.DependencyUnbound)
	}
	locks, err := s.deps.References.ReferenceLocks(ctx, a, p, refs)
	if err != nil {
		return nil, err
	}
	ids, err := meetingAttachmentIDs(nodes)
	if err != nil {
		return nil, err
	}
	if len(ids) != 0 {
		if nilPort(s.deps.Attachments) {
			return nil, fault(f.DependencyUnbound)
		}
		more, err := s.deps.Attachments.AttachmentLocks(ctx, a, p, ids)
		if err != nil {
			return nil, portError(err)
		}
		locks = append(locks, more...)
	}
	return normalize(locks)
}
func (s *Service) requireReferences(ctx context.Context, tx f.Tx, a i.Actor, p i.ProjectID, nodes []c.InlineNode) error {
	refs := references(nodes)
	if len(refs) == 0 {
		return nil
	}
	if nilPort(s.deps.References) {
		return fault(f.DependencyUnbound)
	}
	return portError(s.deps.References.RequireReferencesInTx(ctx, tx, a, p, refs))
}

// Start is the only Human new-session boundary. The session, participants,
// first message and first turn commit together or do not become visible.
func (s *Service) Start(ctx context.Context, a i.Actor, r c.StartRequest) (c.Receipt, error) {
	if ctx == nil || s == nil {
		return c.Receipt{}, fault(f.InvalidArgument)
	}
	if err := human(a); err != nil {
		return c.Receipt{}, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, err
	}
	d, err := commandDigest(a, "start", r)
	if err != nil {
		return c.Receipt{}, err
	}
	command, err := commandIdentity(r.ProjectID, r.Meta.IdempotencyKey)
	if err != nil {
		return c.Receipt{}, portError(err)
	}
	// An authorized replay is resolved before re-reading current Agent choices.
	if receipt, found, err := s.lookup(ctx, a, r.ProjectID, r.Meta.IdempotencyKey, d); err != nil || found {
		return receipt, err
	}
	entries := make([]ac.DirectoryEntry, len(r.Agents))
	for n, id := range r.Agents {
		entries[n], err = s.deps.Directory.GetAgent(ctx, a, r.ProjectID, id)
		if err != nil {
			return c.Receipt{}, portError(err)
		}
	}
	id, err := f.NewID[c.Meeting]()
	if err != nil {
		return c.Receipt{}, portError(err)
	}
	locks := []f.LockRequest{userLock(a), projectLock(r.ProjectID, f.Shared), commandLock(command), meetingLock(id, f.Exclusive)}
	schedule, _ := f.ProjectScheduleLock(r.ProjectID.String())
	locks = append(locks, f.LockRequest{Key: schedule, Mode: f.Exclusive})
	for _, agent := range r.Agents {
		locks = append(locks, agentLock(agent))
	}
	extra, err := s.referenceLocks(ctx, a, r.ProjectID, r.Content)
	if err != nil {
		return c.Receipt{}, err
	}
	locks, err = normalize(append(locks, extra...))
	if err != nil {
		return c.Receipt{}, portError(err)
	}
	cause, _ := f.NewCommandsCause(command)
	var out c.Receipt
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, a, r.ProjectID, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if old, found, err := lookupCommand(ctx, x, r.ProjectID, a, r.Meta.IdempotencyKey, d); err != nil || found {
			out = old
			return err
		}
		if err = s.owner(ctx, tx, a, r.ProjectID, i.Mutate); err != nil {
			return err
		}
		for n, agent := range r.Agents {
			current, err := s.deps.Agents.RequireCurrentInTx(ctx, tx, a, r.ProjectID, agent)
			if err != nil {
				return portError(err)
			}
			if current.ConfigVersion != entries[n].Version {
				return fault(f.VersionConflict)
			}
		}
		if err = s.requireReferences(ctx, tx, a, r.ProjectID, r.Content); err != nil {
			return err
		}
		at := now()
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.sessions(id,project_id,status,version,created_at,updated_at) VALUES($1,$2,'active',1,$3,$3)`, id.String(), r.ProjectID.String(), at); err != nil {
			return portError(err)
		}
		participants := make([]c.ParticipantView, 0, len(entries)+1)
		userID, err := f.NewID[c.Participant]()
		if err != nil {
			return portError(err)
		}
		participants = append(participants, c.ParticipantView{ID: userID, MeetingID: id, Kind: "user", SourceID: a.Details().UserID, Name: "User", Order: 0, Active: true})
		for n, e := range entries {
			pid, err := f.NewID[c.Participant]()
			if err != nil {
				return portError(err)
			}
			name := e.Name
			if e.DisplayName != nil {
				name = *e.DisplayName
			}
			participants = append(participants, c.ParticipantView{ID: pid, MeetingID: id, Kind: "agent", SourceID: e.ID.String(), Name: name, Description: e.Description, Order: n + 1, Active: true})
		}
		for _, p := range participants {
			if err = insertParticipant(ctx, x, p, at); err != nil {
				return err
			}
		}
		nodes, err := normalizeMentions(r.Content, participants, true)
		if err != nil {
			return err
		}
		turn, err := s.insertTurn(ctx, tx, x, a, r.ProjectID, id, r.Mode, nodes, participants, at, nil)
		if err != nil {
			return err
		}
		out = c.Receipt{MeetingID: id, TurnID: &turn, Version: 1}
		return saveCommand(ctx, x, r.ProjectID, a, r.Meta.IdempotencyKey, "start", d, out)
	})
	if err = commitError(result); err != nil {
		return c.Receipt{}, err
	}
	s.invalidateMeeting(r.ProjectID, out.MeetingID)
	return out, nil
}
func (s *Service) lookup(ctx context.Context, a i.Actor, p i.ProjectID, key f.IdempotencyKey, d f.Digest) (c.Receipt, bool, error) {
	command, err := commandIdentity(p, key)
	if err != nil {
		return c.Receipt{}, false, portError(err)
	}
	locks, err := normalize([]f.LockRequest{userLock(a), projectLock(p, f.Shared), commandLock(command)})
	if err != nil {
		return c.Receipt{}, false, portError(err)
	}
	cause, _ := f.NewCommandsCause(command)
	var out c.Receipt
	found := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, a, p, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		out, found, err = lookupCommand(ctx, x, p, a, key, d)
		return err
	})
	if err = commitError(result); err != nil {
		return c.Receipt{}, false, err
	}
	if found {
		// A conclusive original receipt also repairs a notification missed
		// while the earlier command's COMMIT response was unknown.
		s.invalidateMeeting(p, out.MeetingID)
	}
	return out, found, nil
}
func (s *Service) LookupStart(ctx context.Context, a i.Actor, r c.StartRequest) (c.Receipt, bool, error) {
	if err := human(a); err != nil {
		return c.Receipt{}, false, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, false, err
	}
	d, err := commandDigest(a, "start", r)
	if err != nil {
		return c.Receipt{}, false, err
	}
	return s.lookup(ctx, a, r.ProjectID, r.Meta.IdempotencyKey, d)
}
func (s *Service) Send(ctx context.Context, a i.Actor, r c.SendRequest) (c.Receipt, error) {
	if ctx == nil || s == nil {
		return c.Receipt{}, fault(f.InvalidArgument)
	}
	if err := human(a); err != nil {
		return c.Receipt{}, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, err
	}
	d, err := commandDigest(a, "send", r)
	if err != nil {
		return c.Receipt{}, err
	}
	command, err := commandIdentity(r.ProjectID, r.Meta.IdempotencyKey)
	if err != nil {
		return c.Receipt{}, err
	}
	locks := []f.LockRequest{userLock(a), projectLock(r.ProjectID, f.Shared), commandLock(command), meetingLock(r.MeetingID, f.Exclusive)}
	extra, err := s.referenceLocks(ctx, a, r.ProjectID, r.Content)
	if err != nil {
		return c.Receipt{}, err
	}
	locks, err = normalize(append(locks, extra...))
	if err != nil {
		return c.Receipt{}, err
	}
	cause, _ := f.NewCommandsCause(command)
	var out c.Receipt
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, a, r.ProjectID, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if old, found, err := lookupCommand(ctx, x, r.ProjectID, a, r.Meta.IdempotencyKey, d); err != nil || found {
			out = old
			return err
		}
		if err = s.owner(ctx, tx, a, r.ProjectID, i.Mutate); err != nil {
			return err
		}
		current, err := loadSession(ctx, x, r.ProjectID, r.MeetingID)
		if err != nil {
			return err
		}
		if current.Version != r.ExpectedVersion {
			return fault(f.VersionConflict)
		}
		if current.Status != "active" || current.Deleting {
			return fault(f.InvalidState)
		}
		participants, err := loadParticipants(ctx, x, r.MeetingID)
		if err != nil {
			return err
		}
		nodes, err := normalizeMentions(r.Content, participants, false)
		if err != nil {
			return err
		}
		if err = s.requireReferences(ctx, tx, a, r.ProjectID, nodes); err != nil {
			return err
		}
		if err = s.requireReplyInTx(ctx, tx, r.MeetingID, r.ReplyTo); err != nil {
			return err
		}
		turn, err := s.insertTurn(ctx, tx, x, a, r.ProjectID, r.MeetingID, r.Mode, nodes, participants, now(), r.ReplyTo)
		if err != nil {
			return err
		}
		if r.Interrupt {
			if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.turns SET cancel_requested=true,interrupted_by_id=$2,version=version+1 WHERE meeting_id=$1 AND status IN('running','finalizing')`, r.MeetingID.String(), turn.String()); err != nil {
				return portError(err)
			}
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.sessions SET version=version+1,updated_at=$2 WHERE id=$1`, r.MeetingID.String(), now()); err != nil {
			return portError(err)
		}
		out = c.Receipt{MeetingID: r.MeetingID, TurnID: &turn, Version: current.Version + 1}
		return saveCommand(ctx, x, r.ProjectID, a, r.Meta.IdempotencyKey, "send", d, out)
	})
	if err = commitError(result); err != nil {
		return c.Receipt{}, err
	}
	s.invalidateMeeting(r.ProjectID, out.MeetingID)
	return out, nil
}
func normalizeMentions(nodes []c.InlineNode, participants []c.ParticipantView, draft bool) ([]c.InlineNode, error) {
	out := slices.Clone(nodes)
	byID := map[string]c.ParticipantView{}
	byAgent := map[string]c.ParticipantView{}
	for _, p := range participants {
		if p.Active {
			byID[p.ID.String()] = p
			if p.Kind == "agent" {
				byAgent[p.SourceID] = p
			}
		}
	}
	for n, node := range out {
		if node.Type != "mention" {
			continue
		}
		if node.AgentID != "" {
			p, ok := byAgent[node.AgentID]
			if !draft || !ok {
				return nil, fault(f.InvalidArgument)
			}
			out[n].ParticipantID = p.ID.String()
			out[n].AgentID = ""
		} else if _, ok := byID[node.ParticipantID]; !ok {
			return nil, fault(f.InvalidArgument)
		}
	}
	return out, nil
}
func insertParticipant(ctx context.Context, x postgres.SQLExecutor, p c.ParticipantView, at time.Time) error {
	_, err := x.Exec(ctx, `INSERT INTO agenteam_meeting.participants(id,meeting_id,kind,source_id,name,description,sort_order,active,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, p.ID.String(), p.MeetingID.String(), p.Kind, p.SourceID, p.Name, p.Description, p.Order, p.Active, at)
	return portError(err)
}

// A reply names an immutable message, including an older visible version, in
// this same Meeting. Only the already authorized Human Send path calls this
// check. Neither a reply ID nor a Participant mention grants another read.
func (s *Service) requireReplyInTx(ctx context.Context, tx f.Tx, meeting c.MeetingID, reply *c.MessageID) error {
	if reply == nil {
		return nil
	}
	if reply.Validate() != nil || meeting.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{meetingLock(meeting, f.Exclusive)}); err != nil {
		return portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	var found bool
	if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.messages WHERE meeting_id=$1 AND id=$2)`, meeting.String(), reply.String()).Scan(&found); err != nil {
		return portError(err)
	}
	if !found {
		return fault(f.NotFound)
	}
	return ctx.Err()
}

func (s *Service) insertTurn(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, actor i.Actor, project i.ProjectID, meeting c.MeetingID, mode string, nodes []c.InlineNode, participants []c.ParticipantView, at time.Time, reply *c.MessageID) (c.TurnID, error) {
	var zero c.TurnID
	turn, err := f.NewID[c.Turn]()
	if err != nil {
		return zero, portError(err)
	}
	message, err := f.NewID[c.Message]()
	if err != nil {
		return zero, portError(err)
	}
	var user c.ParticipantView
	targets := []c.ParticipantView{}
	mentions := map[string]bool{}
	for _, n := range nodes {
		if n.Type == "mention" {
			mentions[n.ParticipantID] = true
		}
	}
	mentionedAgent := false
	for _, p := range participants {
		if p.Active && p.Kind == "agent" && mentions[p.ID.String()] {
			mentionedAgent = true
		}
	}
	for _, p := range participants {
		if !p.Active {
			continue
		}
		if p.Kind == "user" {
			user = p
		} else if !mentionedAgent || mentions[p.ID.String()] {
			targets = append(targets, p)
		}
	}
	if user.ID.Validate() != nil || len(targets) == 0 {
		return zero, fault(f.InvalidArgument)
	}
	body, err := canonical(nodes)
	if err != nil {
		return zero, err
	}
	snapshot, err := json.Marshal(targets)
	if err != nil {
		return zero, err
	}
	var replyID *string
	if reply != nil {
		id := reply.String()
		replyID = &id
	}
	attachments, err := s.captureMeetingAttachments(ctx, tx, x, actor, project, meeting, nodes)
	if err != nil {
		return zero, err
	}
	attachmentData, err := json.Marshal(attachments)
	if err != nil {
		return zero, portError(err)
	}
	if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.messages(id,meeting_id,participant_id,content,content_digest,created_at,reply_to_id,attachment_data) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, message.String(), meeting.String(), user.ID.String(), body, digest(body).String(), at, replyID, attachmentData); err != nil {
		return zero, portError(err)
	}
	for n, node := range nodes {
		if node.Reference != nil {
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.message_references(message_id,node_index,kind,stable_id) VALUES($1,$2,$3,$4)`, message.String(), n, node.Reference.Kind, node.Reference.ID); err != nil {
				return zero, portError(err)
			}
		}
	}
	if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.turns(id,meeting_id,mode,status,version,participants,created_at) VALUES($1,$2,$3,'queued',1,$4,$5)`, turn.String(), meeting.String(), mode, snapshot, at); err != nil {
		return zero, portError(err)
	}
	all := append([]c.ParticipantView{user}, targets...)
	for n, p := range all {
		contribution, err := f.NewID[c.Contribution]()
		if err != nil {
			return zero, portError(err)
		}
		state := "pending"
		var msg *string
		var completed *time.Time
		if n == 0 {
			state = "completed"
			v := message.String()
			msg = &v
			completed = &at
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.contributions(id,meeting_id,turn_id,participant_id,kind,sort_order,status,current_message_id,completed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, contribution.String(), meeting.String(), turn.String(), p.ID.String(), p.Kind, n, state, msg, completed); err != nil {
			return zero, portError(err)
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.timeline_items(contribution_id,meeting_id,occurred_at,version) VALUES($1,$2,$3,1)`, contribution.String(), meeting.String(), at); err != nil {
			return zero, portError(err)
		}
	}
	return turn, nil
}
