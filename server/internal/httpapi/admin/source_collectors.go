package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/adminauth"
	"github.com/darrenhoo/nex_club/server/internal/automation"
	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/platform/httpx"
	"github.com/darrenhoo/nex_club/server/internal/ports"
	"github.com/darrenhoo/nex_club/server/internal/sources/listfeed"
	"github.com/darrenhoo/nex_club/server/internal/sources/presets"
	"github.com/darrenhoo/nex_club/server/internal/sources/social"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var sourcePreviewSlots = make(chan struct{}, 3)

func sourceCapabilities(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"paid": social.Capabilities(), "preview_limit": 20, "presets_commit": presets.UpstreamCommit, "presets_url": presets.UpstreamURL})
}
func sourcePresets(w http.ResponseWriter, r *http.Request) {
	_, svc, err := automationService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	items, err := svc.Presets(r.Context())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "upstream_commit": presets.UpstreamCommit, "upstream_url": presets.UpstreamURL})
}
func importSourcePresets(w http.ResponseWriter, r *http.Request) {
	d, svc, err := automationService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	raw, err := readRaw(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var input struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(raw, &input); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ExecuteWrite(w, r, "POST /api/admin/source-presets/import", raw, func(ctx context.Context, tx pgx.Tx) (ports.WriteResult, error) {
		result, err := svc.ImportPresetsTx(ctx, tx, input.IDs)
		if err != nil {
			return ports.WriteResult{}, err
		}
		changes, _ := json.Marshal(result)
		auditor := adminauth.NewAuditor(d.Pool)
		if err := auditor.Record(ctx, tx, ports.AuditEvent{ActorType: "admin", ActorAdminID: actorFrom(ctx), Action: "source.import_presets", TargetType: "source_catalog", TargetID: "aihot", Changes: changes, RequestID: httpx.RequestID(ctx)}); err != nil {
			return ports.WriteResult{}, err
		}
		return jsonResult(http.StatusOK, result)
	})
}
func previewSource(w http.ResponseWriter, r *http.Request) {
	d, _, err := automationService()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if d.Ingest == nil {
		httpx.WriteError(w, r, apperr.Unavailable("采集服务未装配"))
		return
	}
	raw, err := readRaw(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var input automation.SourceInput
	if err := decodeJSON(raw, &input); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	cfg, err := input.PreviewConfig()
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if input.Kind == "external" {
		httpx.WriteError(w, r, apperr.Invalid("外部推送不支持主动试抓"))
		return
	}
	actor, ok := AdminID(r.Context())
	if !ok {
		httpx.WriteError(w, r, apperr.Unauthenticated("未登录"))
		return
	}
	requestKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(requestKey) > 128 {
		httpx.WriteError(w, r, apperr.Invalid("试抓请求编号过长"))
		return
	}
	if requestKey == "" {
		if input.Kind == "x" || input.Kind == "wechat" {
			httpx.WriteError(w, r, apperr.Invalid("付费试抓需要请求编号，请重新点击试抓"))
			return
		}
		requestKey = uuid.NewString()
	}
	digest := sha256.Sum256([]byte(actor.String() + "\x00" + requestKey))
	previewKey := "preview:" + hex.EncodeToString(digest[:])
	select {
	case sourcePreviewSlots <- struct{}{}:
		defer func() { <-sourcePreviewSlots }()
	default:
		httpx.WriteError(w, r, apperr.RateLimited("正在试抓的请求较多，请稍后重试"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	started := time.Now()
	batch, err := d.Ingest.Preview(ctx, ingest.Source{ID: uuid.New(), Key: "preview", Kind: input.Kind, Name: input.Name, Config: cfg, PreviewKey: previewKey, Mode: ingest.ModeInternal})
	if err != nil {
		var permanent *ingest.PermanentError
		var transient *ingest.RetryableError
		switch {
		case errors.As(err, &permanent):
			httpx.WriteError(w, r, apperr.New(permanent.Code, permanent.Error(), http.StatusBadRequest))
		case errors.As(err, &transient):
			httpx.WriteError(w, r, apperr.New("upstream_unavailable", transient.Error(), http.StatusBadGateway))
		default:
			httpx.WriteError(w, r, apperr.New("fetch_failed", "试抓失败："+err.Error(), http.StatusBadGateway))
		}
		return
	}
	type item struct {
		Title         string     `json:"title"`
		URL           string     `json:"url"`
		Summary       string     `json:"summary"`
		PublishedAt   *time.Time `json:"published_at"`
		PublishedDate string     `json:"published_date,omitempty"`
		Author        string     `json:"author"`
	}
	items := []item{}
	bad := 0
	for i, row := range batch.Items {
		if row.BadDate {
			bad++
		}
		if i >= 20 {
			continue
		}
		summary := row.Excerpt
		if summary == "" {
			summary = row.BodyText
		}
		dateOnly := ""
		if row.PublishedDateOnly && row.PublishedAt != nil {
			dateOnly = row.PublishedAt.Format("2006-01-02")
		}
		items = append(items, item{PublishedDate: dateOnly, Title: listfeed.Clip(row.Title, 1000), URL: row.URL, Summary: listfeed.Clip(summary, 1200), PublishedAt: row.PublishedAt, Author: row.Author})
	}
	warnings := []string{}
	if bad > 0 {
		warnings = append(warnings, "部分日期无法解析，已保留为未知；没有用抓取时间代替原文日期。")
	}
	if input.Kind == "wechat" {
		warnings = append(warnings, "公众号试抓仅查询文章列表；正式采集新文章时还会按预算请求正文。")
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"total": len(batch.Items), "items": items, "warnings": warnings, "elapsed_ms": time.Since(started).Milliseconds(), "persisted_items": 0, "processing_started": false, "paid": input.Kind == "x" || input.Kind == "wechat"})
}
