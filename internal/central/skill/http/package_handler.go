package skillhttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type packageOpener interface {
	OpenPackage(context.Context, id.Actor, id.ProjectID, sc.SkillID, f.Revision) (*sc.PackageReader, error)
}

type packageRoute struct {
	project, skill, revision string
	matched                  bool
}

func parsePackageRoute(path string) packageRoute {
	if !strings.HasPrefix(path, projectPrefix) {
		return packageRoute{}
	}
	p := strings.Split(strings.TrimPrefix(path, projectPrefix), "/")
	if len(p) != 6 || p[0] == "" || p[1] != "skills" || p[2] == "" || p[3] != "revisions" || p[4] == "" || p[5] != "package" {
		return packageRoute{}
	}
	return packageRoute{p[0], p[2], p[4], true}
}

// Package responses reuse the original request lifetime and the caller-owned
// Skill service. No response is committed before the reader's actual Close.
func (h *handler) servePackage(w http.ResponseWriter, request *http.Request, route packageRoute) {
	ctx, cancel := context.WithTimeout(request.Context(), readBudget)
	defer cancel()
	request.Pattern = projectPrefix + "{project_id}/skills/{skill_id}/revisions/{revision}/package"
	r := request.WithContext(ctx)
	w = abortWriter{w}
	native := &nativeWriter{ResponseWriter: w}
	budget := requestIO{controller: http.NewResponseController(native), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := budget.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !native.prepare() || budget.start() != nil {
		panic(http.ErrAbortHandler)
	}
	body, err := h.executePackage(w, r, route)
	if expired(ctx) || budget.closeBody() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
	} else {
		w.Header().Set("Content-Type", sc.PackageMediaType)
		w.Header().Set("Content-Disposition", `attachment; filename="skill-package.zip"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}
	if expired(ctx) || budget.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *handler) executePackage(w http.ResponseWriter, r *http.Request, route packageRoute) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human {
		return nil, f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	if httpapi.RequestID(r.Context()).Validate() != nil {
		return nil, invalidInput()
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[id.Project](route.project)
	if err != nil || project.String() != route.project {
		return nil, invalidInput()
	}
	target, err := f.ParseID[pc.Skill](route.skill)
	if err != nil || target.String() != route.skill {
		return nil, invalidInput()
	}
	revision, err := f.ParseRevision(route.revision)
	if err != nil || revision.String() != route.revision || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalidInput()
	}
	for _, name := range []string{"Content-Encoding", "Range", "If-Range"} {
		if len(r.Header.Values(name)) != 0 {
			return nil, invalidInput()
		}
	}
	if err := emptyBody(r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	opener, ok := h.reader.(packageOpener)
	if !ok {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	body, err := opener.OpenPackage(r.Context(), actor, project, target, revision)
	if err != nil {
		if body != nil {
			err = errors.Join(body.Close(), err)
		}
		return nil, err
	}
	if body == nil {
		return nil, badProjection()
	}
	return verifiedPackage(r.Context(), body, project, target, revision)
}

func verifiedPackage(ctx context.Context, body *sc.PackageReader, project id.ProjectID, target sc.SkillID, revision f.Revision) (raw []byte, err error) {
	defer func() {
		// A release Unknown is preserved ahead of any local integrity/read
		// failure. Local Joined does not certify the durable D05 lease outcome.
		if closeErr := body.Close(); closeErr != nil {
			raw, err = nil, errors.Join(closeErr, err)
		}
		if !body.Joined() {
			raw, err = nil, errors.Join(err, badProjection())
		}
	}()
	meta, err := body.Metadata()
	if err != nil || meta.Validate() != nil || meta.ProjectID != project || meta.SkillID != target || meta.Revision != revision {
		return nil, badProjection()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err = io.ReadAll(io.LimitReader(body, int64(meta.Object.ByteSize)+1))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(raw)) != int64(meta.Object.ByteSize) {
		return nil, badProjection()
	}
	digest := sha256.Sum256(raw)
	if f.Digest("sha256:"+hex.EncodeToString(digest[:])) != meta.PackageSHA256 {
		return nil, badProjection()
	}
	pkg, err := skill.ParseCanonicalPackage(ctx, raw)
	if err != nil {
		return nil, badProjection()
	}
	manifest, err := pkg.Manifest()
	if err != nil {
		return nil, badProjection()
	}
	want, err := meta.Manifest.Digest()
	if err != nil {
		return nil, badProjection()
	}
	got, err := manifest.Digest()
	if err != nil || got != want {
		return nil, badProjection()
	}
	entry, err := pkg.Entry()
	if err != nil || entry.Name != meta.Name || entry.Description != meta.Description {
		return nil, badProjection()
	}
	return raw, nil
}
