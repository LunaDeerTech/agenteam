package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// Downloads is a trusted composition port. It installs no HTTP route or
// identity fallback. A future authenticated handler supplies the actual Human.
type Downloads struct{ data func() downloadService }
type downloadService struct {
	objects  *Service
	provider oc.DownloadProvider
	keys     DownloadKeyring
}

func NewDownloads(objects *Service, provider oc.DownloadProvider, keys DownloadKeyring) (*Downloads, error) {
	if objects == nil || objects.data == nil || keys.Validate() != nil {
		return nil, invalid()
	}
	d := downloadService{objects, provider, keys}
	return &Downloads{func() downloadService { return d }}, nil
}
func (d *Downloads) state() downloadService    { return d.data() }
func (Downloads) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_downloads") }
func (Downloads) MarshalJSON() ([]byte, error) { return []byte(`"object_downloads"`), nil }
func (*Downloads) UnmarshalJSON([]byte) error  { return invalid() }
func (Downloads) LogValue() slog.Value         { return slog.StringValue("object_downloads") }
func downloadHuman(actor identity.Actor) error {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human {
		return failure(foundation.Forbidden, nil)
	}
	return nil
}
func downloadGrantLock(id oc.DownloadGrantID) foundation.LockRequest {
	k, _ := foundation.RecordLock(foundation.ReferenceRecordLock, "download:"+id.String())
	return foundation.LockRequest{Key: k, Mode: foundation.Exclusive}
}
func (d *Downloads) within(ctx context.Context, actor identity.Actor, c downloadClaims, fn func(context.Context, foundation.Tx, postgres.SQLExecutor, oc.AccessLockPlan, oc.LockedAccess) error) foundation.CommitResult {
	reject := func(err error) foundation.CommitResult {
		var f *foundation.Fault
		if !errors.As(err, &f) {
			f = foundation.NewFault(foundation.DependencyUnavailable, foundation.NotCommitted).WithCause(err)
		}
		return foundation.NotCommittedResult(f)
	}
	if err := downloadHuman(actor); err != nil {
		return reject(err)
	}
	if c.user.String() != actor.Details().UserID {
		return reject(failure(foundation.Forbidden, nil))
	}
	r := d.state()
	if nilPort(r.provider) {
		return reject(failure(foundation.DependencyUnbound, nil))
	}
	req, err := oc.NewSourceAccess(oc.AccessRequestDetails{Operation: oc.ValidateSourceAccess, Actor: actor, Source: c.target.Details().Source})
	if err != nil {
		return reject(err)
	}
	plan, err := r.objects.DiscoverAccess(ctx, req)
	if err != nil {
		return reject(err)
	}
	return r.objects.state().store.WithinTx(ctx, recoveryCause(), func(ctx context.Context, tx foundation.Tx) error {
		locked, err := r.objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, []foundation.LockRequest{downloadGrantLock(c.grant)})
		if err != nil {
			return err
		}
		if err = r.objects.ValidateAccessPlanInTx(ctx, tx, req, plan, locked); err != nil {
			return err
		}
		if err = r.provider.ValidateDownloadInTx(ctx, tx, actor, c.target, plan, locked); err != nil {
			return portError(err)
		}
		e, err := executor(r.objects, tx)
		if err != nil {
			return err
		}
		return fn(ctx, tx, e, plan, locked)
	})
}
func grantWire(c downloadClaims) ([]byte, []byte, error) {
	w, err := c.wire()
	if err != nil {
		return nil, nil, err
	}
	raw, err := canonicalDownload(w)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(raw)
	return raw, sum[:], nil
}
func checkGrant(ctx context.Context, e postgres.SQLExecutor, c downloadClaims, requireLive bool) error {
	raw, hash, err := grantWire(c)
	if err != nil {
		return err
	}
	var stored, actualHash []byte
	var user, object string
	var expires time.Time
	var revoked bool
	err = e.QueryRow(ctx, `SELECT binding,binding_sha256,user_id::text,object_id::text,expires_at,revoked FROM agenteam_download.grants WHERE id=$1`, c.grant.String()).Scan(&stored, &actualHash, &user, &object, &expires, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return failure(foundation.Forbidden, nil)
	}
	if err != nil {
		return unavailable(err)
	}
	normalized, err := cursor.CanonicalJSON(stored)
	if err != nil || !bytes.Equal(raw, normalized) || !bytes.Equal(hash, actualHash) || user != c.user.String() || object != c.target.Details().Source.Details().Meta.ID.String() || !expires.Equal(c.expires.Time()) {
		return failure(foundation.Forbidden, nil)
	}
	if requireLive && (revoked || !time.Now().Before(expires)) {
		return failure(foundation.Forbidden, nil)
	}
	return nil
}
func readGrant(ctx context.Context, e postgres.SQLExecutor, id oc.DownloadGrantID) (downloadClaims, error) {
	var raw []byte
	err := e.QueryRow(ctx, `SELECT binding FROM agenteam_download.grants WHERE id=$1`, id.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return downloadClaims{}, failure(foundation.Forbidden, nil)
	}
	if err != nil {
		return downloadClaims{}, unavailable(err)
	}
	normalized, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return downloadClaims{}, unavailable(err)
	}
	var w downloadWire
	if err = decodeCanonicalDownload(normalized, &w); err != nil {
		return downloadClaims{}, unavailable(err)
	}
	return w.claims()
}
func (d *Downloads) IssueDownload(ctx context.Context, actor identity.Actor, ref oc.BusinessFileRef, mode oc.DownloadMode, expiresIn time.Duration) (oc.PrivateSignedURL, error) {
	if err := downloadHuman(actor); err != nil {
		return oc.PrivateSignedURL{}, err
	}
	if ref.Validate() != nil || !mode.Valid() {
		return oc.PrivateSignedURL{}, invalid()
	}
	if expiresIn == 0 {
		expiresIn = time.Minute
	}
	if expiresIn <= 0 || expiresIn > 5*time.Minute {
		return oc.PrivateSignedURL{}, invalid()
	}
	r := d.state()
	if nilPort(r.provider) {
		return oc.PrivateSignedURL{}, failure(foundation.DependencyUnbound, nil)
	}
	op, finish, err := r.objects.begin(ctx)
	if err != nil {
		return oc.PrivateSignedURL{}, err
	}
	defer finish()
	ctx = op.ctx
	target, err := r.provider.ResolveDownload(ctx, actor, ref)
	if err != nil {
		return oc.PrivateSignedURL{}, portError(err)
	}
	if target.Validate() != nil || encodeDownloadRef(target.Details().Source.Details().Reference) != encodeDownloadRef(ref) {
		return oc.PrivateSignedURL{}, failure(foundation.Forbidden, nil)
	}
	grant, err := foundation.NewID[oc.DownloadGrant]()
	if err != nil {
		return oc.PrivateSignedURL{}, unavailable(err)
	}
	user, _ := foundation.ParseID[identity.User](actor.Details().UserID)
	expires, err := foundation.NewInstant(time.Now().Add(expiresIn))
	if err != nil {
		return oc.PrivateSignedURL{}, invalid()
	}
	c := downloadClaims{grant, user, target, mode, expires}
	raw, hash, err := grantWire(c)
	if err != nil {
		return oc.PrivateSignedURL{}, err
	}
	result := d.within(ctx, actor, c, func(ctx context.Context, tx foundation.Tx, e postgres.SQLExecutor, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		if !time.Now().Before(expires.Time()) {
			return failure(foundation.Forbidden, nil)
		}
		_, err := e.Exec(ctx, `INSERT INTO agenteam_download.grants(id,project_id,user_id,object_id,binding,binding_sha256,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, grant.String(), nullDownload(target.Details().Source.Details().Owner.Details().ProjectID), user.String(), target.Details().Source.Details().Meta.ID.String(), string(raw), hash, expires.Time())
		if err != nil {
			return unavailable(err)
		}
		return r.provider.AppendDownloadInTx(ctx, tx, actor, target, oc.DownloadEvent{GrantID: grant, Phase: oc.DownloadIssued, Length: target.Details().Source.Details().Meta.ByteSize}, plan, locked)
	})
	if result.State() == foundation.Unknown {
		// Acquiring the same EX record lock waits for the original writer's actual
		// transaction outcome. A missing row or an unknown verification yields no URL.
		result = d.within(ctx, actor, c, func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, _ oc.AccessLockPlan, _ oc.LockedAccess) error {
			return checkGrant(ctx, e, c, true)
		})
		if result.State() != foundation.Committed {
			return oc.PrivateSignedURL{}, failure(foundation.CommitUnknown, nil)
		}
	}
	if err = commitError(result); err != nil {
		return oc.PrivateSignedURL{}, err
	}
	if !time.Now().Before(c.expires.Time()) {
		return oc.PrivateSignedURL{}, failure(foundation.Forbidden, nil)
	}
	token, err := r.keys.signDownload(c)
	if err != nil {
		return oc.PrivateSignedURL{}, err
	}
	return oc.NewPrivateSignedURL(user, token, expires)
}
func nullDownload(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (d *Downloads) RevokeDownload(ctx context.Context, actor identity.Actor, id oc.DownloadGrantID) error {
	if downloadHuman(actor) != nil || id.Validate() != nil {
		return failure(foundation.Forbidden, nil)
	}
	r := d.state()
	op, finish, err := r.objects.begin(ctx)
	if err != nil {
		return err
	}
	defer finish()
	c, err := readGrant(op.ctx, r.objects.state().store, id)
	if err != nil {
		return err
	}
	result := d.within(op.ctx, actor, c, func(ctx context.Context, _ foundation.Tx, e postgres.SQLExecutor, _ oc.AccessLockPlan, _ oc.LockedAccess) error {
		if err := checkGrant(ctx, e, c, false); err != nil {
			return err
		}
		_, err := e.Exec(ctx, `UPDATE agenteam_download.grants SET revoked=true WHERE id=$1`, id.String())
		return unavailableIf(err)
	})
	return commitError(result)
}

// Persisted JSON is explicit canonical binding data, never the safe display
// projection of an opaque Actor, reference or DownloadTarget.
