package public

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/darrenhoo/nex_club/server/internal/metrics"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/search"
)

const (
	visitorCookie = "nex_vid"
	visitorMaxAge = 180 * 24 * 60 * 60
	maxEventBytes = 64 << 10
	maxEvents     = 20
)

type handler struct {
	search  *search.Service
	metrics *metrics.Service
}

func (h handler) listResources(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var limit *int
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			httpx.WriteError(w, r, apperr.Invalid("limit 必须是 1 到 60 的整数", apperr.FieldError{Field: "limit", Code: "invalid"}))
			return
		}
		limit = &n
	}
	page, err := h.search.List(r.Context(), search.ListRequest{
		Kind:   q.Get("kind"),
		Q:      q.Get("q"),
		Tags:   q["tag"],
		Sort:   q.Get("sort"),
		Cursor: q.Get("cursor"),
		Limit:  limit,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (h handler) getResource(w http.ResponseWriter, r *http.Request) {
	detail, err := h.search.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (h handler) getResourceBySlug(w http.ResponseWriter, r *http.Request) {
	detail, err := h.search.GetBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (h handler) listTags(w http.ResponseWriter, r *http.Request) {
	page, err := h.search.Tags(r.Context(), r.URL.Query().Get("kind"), r.URL.Query().Get("q"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (h handler) listFeatured(w http.ResponseWriter, r *http.Request) {
	page, err := h.search.Featured(r.Context(), r.URL.Query().Get("kind"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (h handler) postEvents(w http.ResponseWriter, r *http.Request) {
	token, issued := visitorToken(r)
	if issued {
		http.SetCookie(w, &http.Cookie{
			Name:     visitorCookie,
			Value:    token,
			Path:     "/",
			MaxAge:   visitorMaxAge,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}
	events, err := decodeEvents(w, r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	result, err := h.metrics.Record(r.Context(), token, events)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, result)
}

type eventDTO struct {
	ID         string `json:"id"`
	ResourceID string `json:"resource_id"`
	Type       string `json:"type"`
}

func decodeEvents(w http.ResponseWriter, r *http.Request) ([]metrics.Event, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEventBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var body struct {
		Events *[]eventDTO `json:"events"`
	}
	if err := dec.Decode(&body); err != nil {
		return nil, apperr.Invalid("事件格式无效", apperr.FieldError{Field: "events", Code: "invalid"})
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, apperr.Invalid("事件格式无效", apperr.FieldError{Field: "events", Code: "invalid"})
	}
	if body.Events == nil {
		return nil, apperr.Invalid("事件格式无效", apperr.FieldError{Field: "events", Code: "invalid"})
	}
	if len(*body.Events) > maxEvents {
		return nil, apperr.Invalid("单次最多提交 20 条事件", apperr.FieldError{Field: "events", Code: "invalid"})
	}
	out := make([]metrics.Event, 0, len(*body.Events))
	for _, item := range *body.Events {
		id, err := uuid.Parse(strings.TrimSpace(item.ID))
		if err != nil {
			return nil, apperr.Invalid("事件格式无效", apperr.FieldError{Field: "id", Code: "invalid"})
		}
		resourceID, err := uuid.Parse(strings.TrimSpace(item.ResourceID))
		if err != nil {
			return nil, apperr.Invalid("事件格式无效", apperr.FieldError{Field: "resource_id", Code: "invalid"})
		}
		if item.Type != "detail_view" && item.Type != "outbound_click" {
			return nil, apperr.Invalid("事件格式无效", apperr.FieldError{Field: "type", Code: "invalid"})
		}
		out = append(out, metrics.Event{ID: id, ResourceID: resourceID, Type: item.Type})
	}
	return out, nil
}

func visitorToken(r *http.Request) (string, bool) {
	if cookie, err := r.Cookie(visitorCookie); err == nil && validVisitor(cookie.Value) {
		return cookie.Value, false
	}
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return strings.Repeat("0", 64), true
	}
	return hex.EncodeToString(buf[:]), true
}

func validVisitor(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
