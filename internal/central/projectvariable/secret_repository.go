package projectvariable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type secretVariableRow struct {
	Variable          c.SecretVariable
	Deleted           *f.Instant
	Ref               sc.CredentialRef
	CredentialVersion f.Version
}

func (secretVariableRow) Format(w fmt.State, _ rune) { secretRecordSafe(w) }
func (secretVariableRow) LogValue() slog.Value       { return slog.StringValue("secret_variable_record") }
func (secretVariableRow) MarshalJSON() ([]byte, error) {
	return []byte(`"secret_variable_record"`), nil
}

const secretVariableColumns = `id::text,project_id::text,type,name,description,version,created_at,updated_at,deleted_at,credential_id::text,credential_version`

func scanSecretVariable(row interface{ Scan(...any) error }) (*secretVariableRow, error) {
	var id, project, typ, name, description string
	var version int64
	var created, updated time.Time
	var deleted *time.Time
	var credential *string
	var credentialVersion *int64
	if err := row.Scan(&id, &project, &typ, &name, &description, &version, &created, &updated, &deleted, &credential, &credentialVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fault(f.NotFound)
		}
		return nil, unavailable(err)
	}
	fields := c.SecretVariableFields{Type: typ, Name: name, Description: description, Version: f.Version(version)}
	var err error
	if fields.ID, err = f.ParseID[i.ProjectVariable](id); err != nil {
		return nil, internal(err)
	}
	if fields.ProjectID, err = f.ParseID[i.Project](project); err != nil {
		return nil, internal(err)
	}
	if fields.CreatedAt, err = f.NewInstant(created); err != nil {
		return nil, internal(err)
	}
	if fields.UpdatedAt, err = f.NewInstant(updated); err != nil {
		return nil, internal(err)
	}
	variable, err := c.NewSecretVariable(fields)
	if err != nil {
		return nil, internal(err)
	}
	out := &secretVariableRow{Variable: variable}
	if deleted != nil {
		at, err := f.NewInstant(*deleted)
		if err != nil || version < 2 || !deleted.Equal(updated) || description != "" || credential != nil || credentialVersion != nil {
			return nil, internal(nil)
		}
		out.Deleted = &at
		return out, nil
	}
	if credential == nil || credentialVersion == nil || *credentialVersion < 1 {
		return nil, internal(nil)
	}
	credentialID, err := f.ParseID[sc.Credential](*credential)
	if err != nil {
		return nil, internal(err)
	}
	scope, err := i.InProject(fields.ProjectID)
	if err != nil {
		return nil, internal(err)
	}
	out.Ref, err = sc.NewCredentialRef(credentialID, scope)
	if err != nil {
		return nil, internal(err)
	}
	out.CredentialVersion = f.Version(*credentialVersion)
	return out, nil
}

func loadSecretVariable(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, id c.VariableID, includeDeleted bool) (*secretVariableRow, error) {
	row, err := scanSecretVariable(x.QueryRow(ctx, `SELECT `+secretVariableColumns+` FROM agenteam_projectvariable.variables WHERE project_id=$1 AND id=$2 AND type='secret'`, project.String(), id.String()))
	if err == nil && row.Deleted != nil && !includeDeleted {
		return nil, fault(f.NotFound)
	}
	return row, err
}

const secretCommandColumns = `id::text,project_id::text,actor_user_id::text,command_name,idempotency_key,target_id::text,expected_version,name_present,description_present,value_present,d04_receipt_id::text,credential_id::text,credential_version,secret_effect,secret_deleted,receipt,committed_at,event_id::text,audit_id::text`

func scanSecretCommand(row interface{ Scan(...any) error }) (*secretCommandRecord, error) {
	var id, project, user, command, key, target, receiptID, credential, effect string
	var expected *int64
	var credentialVersion int64
	var namePresent, descriptionPresent, valuePresent, deleted bool
	var raw []byte
	var committed time.Time
	var eventID, auditID *string
	err := row.Scan(&id, &project, &user, &command, &key, &target, &expected,
		&namePresent, &descriptionPresent, &valuePresent, &receiptID, &credential,
		&credentialVersion, &effect, &deleted, &raw, &committed, &eventID, &auditID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	r := &secretCommandRecord{Command: c.SecretCommandName(command), NamePresent: namePresent, DescriptionPresent: descriptionPresent, ValuePresent: valuePresent}
	if r.ID, err = f.ParseID[c.Operation](id); err != nil {
		return nil, internal(err)
	}
	if r.Project, err = f.ParseID[i.Project](project); err != nil {
		return nil, internal(err)
	}
	if r.User, err = f.ParseID[i.User](user); err != nil {
		return nil, internal(err)
	}
	if r.Target, err = f.ParseID[i.ProjectVariable](target); err != nil {
		return nil, internal(err)
	}
	if r.Key, err = f.ParseIdempotencyKey(key); err != nil {
		return nil, internal(err)
	}
	if expected != nil {
		v := f.Version(*expected)
		r.Expected = &v
	}
	if r.CommittedAt, err = f.NewInstant(committed); err != nil {
		return nil, internal(err)
	}
	if err = r.Receipt.UnmarshalJSON(raw); err != nil {
		return nil, internal(err)
	}
	mutation := r.Receipt.Fields()
	if (eventID == nil) != (mutation.EventID == nil) || (auditID == nil) != (mutation.AuditID == nil) ||
		eventID != nil && *eventID != mutation.EventID.String() || auditID != nil && *auditID != mutation.AuditID.String() {
		return nil, internal(nil)
	}
	observation := sc.ProjectVariableWriteResultFields{ProjectID: r.Project, VariableID: r.Target, UserID: r.User,
		ExpectedVersion: r.Expected, Effect: sc.ProjectVariableEffect(effect), Version: f.Version(credentialVersion), Deleted: deleted}
	if observation.ReceiptID, err = f.ParseID[sc.ProjectVariableReceipt](receiptID); err != nil {
		return nil, internal(err)
	}
	if observation.Identity, err = c.SecretVariableCommandIdentity(r.Project, r.Command, r.Key); err != nil {
		return nil, internal(err)
	}
	if observation.Kind, err = secretMutationKind(r.Command); err != nil {
		return nil, internal(err)
	}
	credentialID, err := f.ParseID[sc.Credential](credential)
	if err != nil {
		return nil, internal(err)
	}
	scope, _ := i.InProject(r.Project)
	if observation.Ref, err = sc.NewCredentialRef(credentialID, scope); err != nil {
		return nil, internal(err)
	}
	if r.Observation, err = sc.NewProjectVariableWriteObservation(observation); err != nil {
		return nil, internal(err)
	}
	if err = validateSecretRecord(r); err != nil {
		return nil, err
	}
	return r, nil
}

func loadSecretCommand(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, command c.SecretCommandName, key f.IdempotencyKey) (*secretCommandRecord, error) {
	return scanSecretCommand(x.QueryRow(ctx, `SELECT `+secretCommandColumns+` FROM agenteam_projectvariable.secret_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, project.String(), string(command), key.String()))
}

func insertSecretCommand(ctx context.Context, x postgres.SQLExecutor, r *secretCommandRecord) error {
	if err := validateSecretRecord(r); err != nil {
		return err
	}
	raw, err := json.Marshal(r.Receipt)
	if err != nil {
		return internal(err)
	}
	result := r.Receipt.Fields()
	observation, _ := r.Observation.Result()
	var expected, eventID, auditID any
	if r.Expected != nil {
		expected = int64(*r.Expected)
	}
	if result.EventID != nil {
		eventID = result.EventID.String()
	}
	if result.AuditID != nil {
		auditID = result.AuditID.String()
	}
	return affected(x.Exec(ctx, `INSERT INTO agenteam_projectvariable.secret_commands(id,project_id,actor_user_id,command_name,idempotency_key,target_id,expected_version,name_present,description_present,value_present,d04_receipt_id,credential_id,credential_version,secret_effect,secret_deleted,event_id,audit_id,receipt,committed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18::jsonb,$19)`,
		r.ID.String(), r.Project.String(), r.User.String(), string(r.Command), r.Key.String(), r.Target.String(), expected,
		r.NamePresent, r.DescriptionPresent, r.ValuePresent, observation.ReceiptID.String(), observation.Ref.Details().ID.String(),
		int64(observation.Version), string(observation.Effect), observation.Deleted, eventID, auditID, string(raw), r.CommittedAt.Time()))
}

func secretGeneration(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID) (int64, error) {
	var generation int64
	if err := x.QueryRow(ctx, `SELECT COALESCE((SELECT query_generation FROM agenteam_projectvariable.secret_project_generations WHERE project_id=$1),1)`, project.String()).Scan(&generation); err != nil {
		return 0, unavailable(err)
	}
	if generation < 1 {
		return 0, internal(nil)
	}
	return generation, nil
}
