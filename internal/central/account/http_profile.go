package account

import (
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

func (h *accountHTTP) httpGetProfile(w http.ResponseWriter, r *http.Request, input httpRequest) {
	out, e := h.profiles.GetProfile(r.Context(), input.actor)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, httpProfileDTO(out))
}
func (h *accountHTTP) httpUpdateProfile(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Version     foundation.Version `json:"version"`
		Username    httpOptionalString `json:"username"`
		DisplayName httpOptionalString `json:"display_name"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	command := c.ProfileChange{ProfileMutation: c.ProfileMutation{Actor: input.actor, Key: input.key, ExpectedVersion: body.Version}, Username: body.Username.value, DisplayName: body.DisplayName.value}
	if e := command.Validate(); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.profiles.UpdateProfile(r.Context(), command)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, httpProfileDTO(out))
}
func httpPreferencesDTO(v c.ProfileView) any {
	return struct {
		Version foundation.Version `json:"version"`
		Theme   c.Theme            `json:"theme"`
	}{v.User.Version, v.User.Theme}
}
func (h *accountHTTP) httpGetPreferences(w http.ResponseWriter, r *http.Request, input httpRequest) {
	out, e := h.profiles.GetProfile(r.Context(), input.actor)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, httpPreferencesDTO(out))
}
func (h *accountHTTP) httpSetPreferences(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body struct {
		Version foundation.Version `json:"version"`
		Theme   c.Theme            `json:"theme"`
	}
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	command := c.ThemeChange{ProfileMutation: c.ProfileMutation{Actor: input.actor, Key: input.key, ExpectedVersion: body.Version}, Theme: body.Theme}
	if e := command.Validate(); e != nil {
		httpProblem(w, r, e)
		return
	}
	out, e := h.profiles.SetTheme(r.Context(), command)
	if e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, httpPreferencesDTO(out))
}
func httpAvatarVersion(r *http.Request) (foundation.Version, error) {
	v, e := httpSingleHeader(r, "If-Match")
	if e != nil || len(v) < 3 || v[0] != '"' || v[len(v)-1] != '"' {
		return 0, field("/version", "INVALID")
	}
	version, e := foundation.ParseVersion(v[1 : len(v)-1])
	if e != nil {
		return 0, field("/version", "INVALID")
	}
	return version, nil
}
func (h *accountHTTP) httpPutAvatar(w http.ResponseWriter, r *http.Request, input httpRequest) {
	transferred := false
	defer func() {
		if !transferred && !nilPort(r.Body) {
			_ = r.Body.Close()
		}
	}()
	version, e := httpAvatarVersion(r)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	typeHeader, e := httpSingleHeader(r, "Content-Type")
	media, params, mediaErr := mime.ParseMediaType(typeHeader)
	if e != nil || mediaErr != nil || len(params) != 0 || len(r.Header.Values("Content-Encoding")) != 0 || media != "image/jpeg" && media != "image/png" && media != "image/webp" {
		httpProblem(w, r, fault(foundation.UnsupportedMediaType, nil))
		return
	}
	if r.ContentLength > accountHTTPAvatarLimit {
		httpProblem(w, r, fault(foundation.PayloadTooLarge, nil))
		return
	}
	if r.ContentLength == 0 || r.ContentLength < -1 || nilPort(r.Body) {
		httpProblem(w, r, invalid())
		return
	}
	command := c.AvatarUpload{ProfileMutation: c.ProfileMutation{Actor: input.actor, Key: input.key, ExpectedVersion: version}, MediaType: media, ByteSize: r.ContentLength, Body: http.MaxBytesReader(w, r.Body, accountHTTPAvatarLimit)}
	if e = command.Validate(); e != nil {
		httpProblem(w, r, e)
		return
	}
	// The service owns the wrapped body from this point, including every error.
	transferred = true
	out, e := h.profiles.PutAvatar(r.Context(), command)
	if e != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(e, &tooLarge) {
			e = fault(foundation.PayloadTooLarge, nil)
		}
		h.httpProblem(w, r, e, true)
		return
	}
	httpJSON(w, r, http.StatusOK, httpProfileDTO(out))
}
func (h *accountHTTP) httpDeleteAvatar(w http.ResponseWriter, r *http.Request, input httpRequest) {
	var body httpVersionInput
	if e := httpapi.DecodeJSON(w, r, &body, accountHTTPJSONLimit); e != nil {
		httpProblem(w, r, e)
		return
	}
	command := c.ProfileMutation{Actor: input.actor, Key: input.key, ExpectedVersion: body.Version}
	if e := command.Validate(); e != nil {
		httpProblem(w, r, e)
		return
	}
	if _, e := h.profiles.DeleteAvatar(r.Context(), command); e != nil {
		h.httpProblem(w, r, e, true)
		return
	}
	httpNoContent(w)
}
func httpRangeNumber(raw string) (int64, error) {
	if raw == "" || len(raw) > 19 {
		return 0, invalid()
	}
	for _, v := range raw {
		if v < '0' || v > '9' {
			return 0, invalid()
		}
	}
	v, e := strconv.ParseInt(raw, 10, 64)
	if e != nil {
		return 0, invalid()
	}
	return v, nil
}
func httpAvatarRange(r *http.Request) (c.AvatarRange, error) {
	values := r.Header.Values("Range")
	if len(values) == 0 {
		return c.AvatarRange{Kind: c.AvatarRangeAll}, nil
	}
	bad := func() (c.AvatarRange, error) { return c.AvatarRange{}, fault(foundation.RangeNotSatisfiable, nil) }
	if len(values) != 1 || !strings.HasPrefix(values[0], "bytes=") {
		return bad()
	}
	parts := strings.Split(strings.TrimPrefix(values[0], "bytes="), "-")
	if len(parts) != 2 {
		return bad()
	}
	var out c.AvatarRange
	if parts[0] == "" {
		n, e := httpRangeNumber(parts[1])
		if e != nil || n == 0 {
			return bad()
		}
		out = c.AvatarRange{Kind: c.AvatarRangeSuffix, Length: n}
	} else {
		start, e := httpRangeNumber(parts[0])
		if e != nil {
			return bad()
		}
		if parts[1] == "" {
			out = c.AvatarRange{Kind: c.AvatarRangeFrom, Offset: start}
		} else {
			end, e := httpRangeNumber(parts[1])
			if e != nil || end < start || end == math.MaxInt64 {
				return bad()
			}
			out = c.AvatarRange{Kind: c.AvatarRangeClosed, Offset: start, Length: end - start + 1}
		}
	}
	if out.Validate() != nil {
		return bad()
	}
	return out, nil
}
func (h *accountHTTP) httpGetAvatar(w http.ResponseWriter, r *http.Request, input httpRequest) {
	requested, e := httpAvatarRange(r)
	if e != nil {
		httpProblem(w, r, e)
		return
	}
	reader, e := h.profiles.ReadAvatar(r.Context(), input.actor, requested)
	if e != nil {
		if reader != nil {
			_ = reader.Close()
		}
		h.httpProblem(w, r, e, true)
		return
	}
	if reader == nil {
		httpProblem(w, r, unavailable(nil))
		return
	}
	closed := false
	defer func() {
		if !closed {
			_ = reader.Close()
		}
	}()
	meta := reader.Meta()
	resolved := reader.Range()
	if meta.Validate() != nil || meta.ByteSize <= 0 || meta.ByteSize > foundation.Progress(accountHTTPAvatarLimit) || meta.MediaType != "image/jpeg" && meta.MediaType != "image/png" && meta.MediaType != "image/webp" || (requested.Kind == c.AvatarRangeAll) != (resolved == nil) {
		httpProblem(w, r, unavailable(nil))
		return
	}
	length := int64(meta.ByteSize)
	status := http.StatusOK
	if resolved != nil {
		length = int64(resolved.Length)
		status = http.StatusPartialContent
		if !httpRangeMatches(requested, int64(resolved.Offset), length, int64(resolved.Total)) {
			httpProblem(w, r, unavailable(nil))
			return
		}
	}
	// HEAD acquires exactly the same current object/lease, then closes it before
	// committing headers. It never reads the payload or manufactures metadata.
	if r.Method == http.MethodHead {
		e = reader.Close()
		closed = true
		if e != nil {
			httpProblem(w, r, unavailable(e))
			return
		}
	}
	w.Header().Set("Content-Type", meta.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("ETag", `"`+meta.SHA256.String()+`"`)
	if resolved != nil {
		w.Header().Set("Content-Range", "bytes "+strconv.FormatInt(int64(resolved.Offset), 10)+"-"+strconv.FormatInt(int64(resolved.Offset)+length-1, 10)+"/"+strconv.FormatInt(int64(resolved.Total), 10))
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_, copyErr := io.CopyN(w, reader, length)
	if copyErr == nil {
		var tail [1]byte
		n, readErr := reader.Read(tail[:])
		if n != 0 || readErr != io.EOF {
			copyErr = io.ErrUnexpectedEOF
		}
	}
	closeErr := reader.Close()
	closed = true
	if copyErr != nil || closeErr != nil {
		panic(http.ErrAbortHandler)
	}
	if e := http.NewResponseController(w).Flush(); e != nil && !errors.Is(e, http.ErrNotSupported) {
		panic(http.ErrAbortHandler)
	}
}
func httpRangeMatches(r c.AvatarRange, offset, length, total int64) bool {
	if total <= 0 || offset < 0 || offset >= total || length <= 0 || length > total-offset {
		return false
	}
	switch r.Kind {
	case c.AvatarRangeClosed:
		return offset == r.Offset && length == min(r.Length, total-offset)
	case c.AvatarRangeFrom:
		return offset == r.Offset && length == total-offset
	case c.AvatarRangeSuffix:
		return length == min(r.Length, total) && offset == total-length
	default:
		return false
	}
}
