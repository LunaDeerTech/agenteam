package account

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/wenlng/go-captcha/v2/base/imagedata"
	"github.com/wenlng/go-captcha/v2/rotate"
)

// Challenges owns only this process's short-lived answers. Construction does no
// I/O; injection is explicit, and an absent provider remains fail closed.
type Challenges struct{ data func() *challengeState }
type challengeState struct {
	authority  *Authority
	process    c.ProcessID
	mu         sync.Mutex
	entries    map[string]*challengeEntry
	generation chan struct{}
}
type challengeEntry struct {
	browser           c.BrowserIdentity
	kid, email, key   string
	expires           time.Time
	angle             int
	verifying, passed bool
}

func NewChallenges(a *Authority, process c.ProcessID) (*Challenges, error) {
	if a == nil || a.data == nil || process.Validate() != nil {
		return nil, invalid()
	}
	st := &challengeState{authority: a, process: process, entries: map[string]*challengeEntry{}, generation: make(chan struct{}, 2)}
	return &Challenges{func() *challengeState { return st }}, nil
}
func (p Challenges) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "account_challenges") }
func (p Challenges) MarshalJSON() ([]byte, error) { return []byte(`"account_challenges"`), nil }
func (p Challenges) LogValue() slog.Value         { return slog.StringValue("account_challenges") }
func challengeLock() foundation.LockRequest {
	return configLock("account-challenges", foundation.Exclusive)
}
func (p *Challenges) pruneLocked(now time.Time) {
	for id, e := range p.data().entries {
		if !now.Before(e.expires) {
			delete(p.data().entries, id)
		}
	}
}

// A passed answer may be forgotten only after its consumption is committed.
// Removing it from CheckLoginInTx would also remove it on transaction rollback.
func (p *Challenges) forgetConsumed(ctx context.Context) error {
	st := p.data()
	st.mu.Lock()
	seen := map[string]*challengeEntry{}
	var ids []string
	for id, e := range st.entries {
		if e.passed {
			ids = append(ids, id)
			seen[id] = e
		}
	}
	st.mu.Unlock()
	if len(ids) == 0 {
		return nil
	}
	rows, e := st.authority.state().store.Query(ctx, `SELECT id::text FROM agenteam_account.challenges WHERE id=ANY($1::uuid[]) AND phase IN ('consumed','failed')`, ids)
	if e != nil {
		return unavailable(e)
	}
	var gone []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			break
		}
		gone = append(gone, id)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return unavailable(e)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, id := range gone {
		if st.entries[id] == seen[id] {
			delete(st.entries, id)
		}
	}
	return nil
}
func (s *Service) challengeProvider() (*Challenges, error) {
	p, ok := s.state().deps.Challenges.(*Challenges)
	if !ok || p == nil || p.data == nil || p.data().authority != s.state().deps.Authority || p.data().process != s.state().process {
		return nil, fault(foundation.DependencyUnbound, nil)
	}
	return p, nil
}
func (s *Service) CreateChallenge(ctx context.Context, r c.ChallengeRequest) (c.Challenge, error) {
	if r.Validate() != nil {
		return c.Challenge{}, invalid()
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return c.Challenge{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	if e = s.requireBrowser(ctx, r.Fields().Browser); e != nil {
		return c.Challenge{}, e
	}
	email, e := NormalizeEmail(r.Fields().Email)
	if e != nil {
		return c.Challenge{}, e
	}
	p, e := s.challengeProvider()
	if e != nil {
		return c.Challenge{}, e
	}
	st := p.data()
	if e = p.forgetConsumed(ctx); e != nil {
		return c.Challenge{}, e
	}
	// Generation is non-cancellable library CPU work. It stays in this registered
	// operation until it actually returns; admission never creates an unbounded queue.
	select {
	case st.generation <- struct{}{}:
		defer func() { <-st.generation }()
	default:
		return c.Challenge{}, fault(foundation.RateLimited, nil)
	}
	id, e := foundation.NewID[c.ChallengeRecord]()
	if e != nil {
		return c.Challenge{}, unavailable(e)
	}
	entry := &challengeEntry{browser: r.Fields().Browser, kid: s.state().keys.current(), email: email, key: string(r.Fields().LoginKey), expires: time.Now().Add(120 * time.Second)}
	st.mu.Lock()
	p.pruneLocked(time.Now())
	n := 0
	for _, v := range st.entries {
		if v.browser.ID() == entry.browser.ID() {
			n++
		}
	}
	if len(st.entries) >= 1024 || n >= 3 {
		st.mu.Unlock()
		return c.Challenge{}, fault(foundation.RateLimited, nil)
	}
	st.entries[id.String()] = entry
	st.mu.Unlock()
	keep := false
	defer func() {
		if !keep {
			st.mu.Lock()
			delete(st.entries, id.String())
			st.mu.Unlock()
		}
	}()
	master, thumb, angle, e := generateChallenge()
	if e != nil {
		return c.Challenge{}, e
	}
	subject, e := s.state().keys.mac(entry.kid, "privacy-counter-v1", []byte("subject"), []byte(email))
	if e != nil {
		return c.Challenge{}, e
	}
	cause, e := recoveryCause("challenge-create")
	if e != nil {
		return c.Challenge{}, e
	}
	var expires time.Time
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{challengeLock()}); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		// SQL is also authoritative for quotas; previously issued passes count until
		// consumed/expired even if the client never returns.
		var total, perBrowser int
		e = x.QueryRow(ctx, `SELECT count(*)::int,count(*) FILTER(WHERE browser_id=$2)::int FROM agenteam_account.challenges WHERE process_id=$1 AND expires_at>clock_timestamp() AND phase IN ('challenge','passed')`, st.process.String(), entry.browser.ID().String()).Scan(&total, &perBrowser)
		if e != nil {
			return unavailable(e)
		}
		if total >= 1024 || perBrowser >= 3 {
			return fault(foundation.RateLimited, nil)
		}
		return portError(x.QueryRow(ctx, `INSERT INTO agenteam_account.challenges(id,process_id,browser_id,purpose,subject_kid,subject_digest,login_key,phase,expires_at) VALUES($1,$2,$3,'login',$4,$5,$6,'challenge',clock_timestamp()+interval '120 seconds') RETURNING expires_at`, id.String(), st.process.String(), entry.browser.ID().String(), entry.kid, subject, entry.key).Scan(&expires))
	})
	if e = resultError(result); e != nil {
		return c.Challenge{}, e
	}
	st.mu.Lock()
	entry.angle = angle
	entry.expires = expires
	st.mu.Unlock()
	keep = true
	return c.Challenge{ID: id, Mode: "rotate", Master: master, Thumb: thumb, ExpiresAt: instant(expires)}, nil
}
func generateChallenge() (string, string, int, error) {
	var seed [24]byte
	if _, e := rand.Read(seed[:]); e != nil {
		return "", "", 0, unavailable(e)
	}
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	// Platform-owned geometric artwork; no downloaded fonts/photos or source
	// image crosses the adapter. Its asymmetry makes every rotation meaningful.
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			c := color.RGBA{uint8(210 + x/16), uint8(216 + y/20), 238, 255}
			for n := 0; n < 6; n++ {
				cx, cy := int(seed[n*4])+100, int(seed[n*4+1])+100
				r := int(seed[n*4+2]%55) + 24
				if (x-cx)*(x-cx)+(y-cy)*(y-cy) < r*r {
					c = color.RGBA{seed[n*4], uint8(80 + seed[n*4+1]/2), seed[n*4+3], 255}
				}
			}
			img.SetRGBA(x, y, c)
		}
	}
	b := rotate.NewBuilder()
	b.SetOptions(rotate.WithImageSquareSize(220), rotate.WithRangeThumbImageSquareSize([]int{160}))
	b.SetResources(rotate.WithImages([]image.Image{img}))
	v, e := b.Make().Generate()
	if e != nil {
		return "", "", 0, unavailable(e)
	}
	return encodeChallengeArtwork(v.GetMasterImage(), v.GetThumbImage(), v.GetData().Angle)
}

func encodeChallengeArtwork(masterImage, thumbImage imagedata.PNGImageData, angle int) (string, string, int, error) {
	if masterImage == nil || thumbImage == nil || masterImage.Get() == nil || thumbImage.Get() == nil || angle < 0 || angle > 360 {
		return "", "", 0, unavailable(nil)
	}
	masterBounds, thumbBounds := masterImage.Get().Bounds(), thumbImage.Get().Bounds()
	if masterBounds.Dx() != 220 || masterBounds.Dy() != 220 {
		return "", "", 0, unavailable(nil)
	}
	if thumbBounds.Dx() != 160 || thumbBounds.Dy() != 160 {
		if (angle != 90 && angle != 180 && angle != 270) || thumbBounds != image.Rect(1, 1, 160, 160) {
			return "", "", 0, unavailable(nil)
		}
		// go-captcha v2.0.5 over-crops cardinal rotations by one final row
		// and column. PNG starts at Bounds.Min: preserve that encoded pixel
		// origin and pad only the missing final edges, without mutating SDK data.
		padded := image.NewNRGBA(image.Rect(0, 0, 160, 160))
		draw.Draw(padded, image.Rect(0, 0, 159, 159), thumbImage.Get(), thumbBounds.Min, draw.Src)
		thumbImage = imagedata.NewPNGImageData(padded)
	}
	master, e := masterImage.ToBase64()
	if e != nil {
		return "", "", 0, unavailable(e)
	}
	thumb, e := thumbImage.ToBase64()
	if e != nil {
		return "", "", 0, unavailable(e)
	}
	if len(master)+len(thumb) > 256<<10 {
		return "", "", 0, unavailable(nil)
	}
	for i, encoded := range []string{master, thumb} {
		const prefix = "data:image/png;base64,"
		if !strings.HasPrefix(encoded, prefix) {
			return "", "", 0, unavailable(nil)
		}
		raw, e := base64.StdEncoding.Strict().DecodeString(encoded[len(prefix):])
		if e != nil {
			return "", "", 0, unavailable(e)
		}
		cfg, e := png.DecodeConfig(bytes.NewReader(raw))
		if e != nil {
			return "", "", 0, unavailable(e)
		}
		want := []int{220, 160}[i]
		if cfg.Width != want || cfg.Height != want {
			return "", "", 0, unavailable(nil)
		}
	}
	return master, thumb, angle, nil
}
func (s *Service) VerifyChallenge(ctx context.Context, r c.ChallengeRequest, id c.ChallengeID, angle int) (sc.SecretMaterial, error) {
	if r.Validate() != nil || id.Validate() != nil {
		return sc.SecretMaterial{}, fault(foundation.ChallengeInvalid, nil)
	}
	op, e := s.begin(ctx, false)
	if e != nil {
		return sc.SecretMaterial{}, e
	}
	defer s.finish(op)
	ctx = op.ctx
	if e = s.requireBrowser(ctx, r.Fields().Browser); e != nil {
		return sc.SecretMaterial{}, e
	}
	email, e := NormalizeEmail(r.Fields().Email)
	if e != nil {
		return sc.SecretMaterial{}, fault(foundation.ChallengeInvalid, nil)
	}
	p, e := s.challengeProvider()
	if e != nil {
		return sc.SecretMaterial{}, e
	}
	st := p.data()
	st.mu.Lock()
	p.pruneLocked(time.Now())
	entry := st.entries[id.String()]
	if entry == nil || entry.verifying || entry.passed || !entry.browser.SameContext(r.Fields().Browser) || entry.email != email || entry.key != string(r.Fields().LoginKey) {
		st.mu.Unlock()
		return sc.SecretMaterial{}, fault(foundation.ChallengeInvalid, nil)
	}
	entry.verifying = true
	valid := angle >= 0 && angle <= 360 && rotate.Validate(angle, entry.angle, 5)
	entry.angle = 0
	st.mu.Unlock()
	raw, e := randomToken()
	if e != nil {
		return sc.SecretMaterial{}, e
	}
	pass := id.String() + "." + raw
	hash := sha256.Sum256([]byte("agenteam.account.challenge-pass.v1\x00" + pass))
	cause, e := recoveryCause("challenge-verify")
	if e != nil {
		return sc.SecretMaterial{}, e
	}
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{challengeLock()}); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		phase := "failed"
		var verifier any
		if valid {
			phase = "passed"
			verifier = hash[:]
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_account.challenges SET phase=$4,pass_verifier=$5,expires_at=CASE WHEN $4='passed' THEN clock_timestamp()+interval '120 seconds' ELSE expires_at END WHERE id=$1 AND process_id=$2 AND browser_id=$3 AND phase='challenge' AND expires_at>clock_timestamp()`, id.String(), st.process.String(), entry.browser.ID().String(), phase, verifier)
		if e != nil {
			return unavailable(e)
		}
		if tag.RowsAffected() != 1 {
			return fault(foundation.ChallengeInvalid, nil)
		}
		return nil
	})
	if e = resultError(result); e != nil {
		return sc.SecretMaterial{}, e
	}
	st.mu.Lock()
	if valid {
		entry.passed = true
		entry.expires = time.Now().Add(120 * time.Second)
	} else {
		delete(st.entries, id.String())
	}
	st.mu.Unlock()
	if !valid {
		return sc.SecretMaterial{}, fault(foundation.ChallengeInvalid, nil)
	}
	return sc.NewSecretMaterial([]byte(pass))
}
func (p *Challenges) PreviewLoginInTx(ctx context.Context, tx foundation.Tx, r c.LoginRequest) error {
	return p.checkLogin(ctx, tx, r, false)
}
func (p *Challenges) CheckLoginInTx(ctx context.Context, tx foundation.Tx, r c.LoginRequest) error {
	return p.checkLogin(ctx, tx, r, true)
}
func (p *Challenges) checkLogin(ctx context.Context, tx foundation.Tx, r c.LoginRequest, consume bool) error {
	if p == nil || p.data == nil || r.Validate() != nil {
		return fault(foundation.ChallengeInvalid, nil)
	}
	st := p.data()
	if e := st.authority.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{challengeLock()}); e != nil {
		return unavailable(e)
	}
	email, e := NormalizeEmail(r.Fields().Email)
	if e != nil {
		return fault(foundation.ChallengeInvalid, nil)
	}
	var id string
	var hash [32]byte
	e = r.Fields().ChallengePass.Use(func(b []byte) error {
		if len(b) > 4096 {
			return invalid()
		}
		parts := strings.Split(string(b), ".")
		if len(parts) != 2 {
			return invalid()
		}
		if _, e := foundation.ParseID[c.ChallengeRecord](parts[0]); e != nil {
			return invalid()
		}
		token, e := base64.RawURLEncoding.Strict().DecodeString(parts[1])
		if e != nil || len(token) != 32 || base64.RawURLEncoding.EncodeToString(token) != parts[1] {
			return invalid()
		}
		id = parts[0]
		hash = sha256.Sum256(append([]byte("agenteam.account.challenge-pass.v1\x00"), b...))
		return nil
	})
	if e != nil {
		return fault(foundation.ChallengeRequired, nil)
	}
	st.mu.Lock()
	entry := st.entries[id]
	ok := entry != nil && entry.passed && entry.browser.SameContext(r.Fields().Browser) && entry.email == email && entry.key == string(r.Fields().Key) && time.Now().Before(entry.expires)
	st.mu.Unlock()
	if !ok {
		return fault(foundation.ChallengeInvalid, nil)
	}
	subject, e := st.authority.state().keys.mac(entry.kid, "privacy-counter-v1", []byte("subject"), []byte(email))
	if e != nil {
		return e
	}
	x, e := st.authority.state().store.InTx(tx)
	if e != nil {
		return unavailable(e)
	}
	var saved []byte
	var matched bool
	e = x.QueryRow(ctx, `SELECT pass_verifier,phase='passed' AND expires_at>clock_timestamp() AND process_id=$2 AND browser_id=$3 AND subject_kid=$4 AND subject_digest=$5 AND login_key=$6 FROM agenteam_account.challenges WHERE id=$1`, id, st.process.String(), entry.browser.ID().String(), entry.kid, subject, entry.key).Scan(&saved, &matched)
	if e != nil || !matched || subtle.ConstantTimeCompare(hash[:], saved) != 1 {
		return fault(foundation.ChallengeInvalid, e)
	}
	if consume {
		_, e = x.Exec(ctx, `UPDATE agenteam_account.challenges SET phase='consumed' WHERE id=$1`, id)
		if e != nil {
			return unavailable(e)
		}
	}
	return nil
}
