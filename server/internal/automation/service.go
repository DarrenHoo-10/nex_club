package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/darrenhoo/nex_club/server/internal/ingest"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	sourceconfig "github.com/darrenhoo/nex_club/server/internal/sources/config"
	"github.com/darrenhoo/nex_club/server/internal/sources/social"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

type Service struct {
	Pool *pgxpool.Pool
	Jobs *river.Client[pgx.Tx]
}

type SourceInput struct {
	Config        json.RawMessage `json:"config"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Repository    string          `json:"repository"`
	FeedURL       string          `json:"feed_url"`
	Mode          string          `json:"participation_mode"`
	Trust         string          `json:"trust_tier"`
	Enabled       bool            `json:"enabled"`
	Interval      int             `json:"interval_seconds"`
	AllowFulltext bool            `json:"allow_fulltext"`
	EditVersion   int64           `json:"edit_version"`
}

var repoPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func (in *SourceInput) validate() (json.RawMessage, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 120 {
		return nil, apperr.Invalid("请填写 1–120 字的信源名称")
	}
	if in.Interval < 300 || in.Interval > 604800 {
		return nil, apperr.Invalid("采集间隔需在 5 分钟至 7 天之间")
	}
	if in.Mode != "content" && in.Mode != "signal" && in.Mode != "internal" {
		return nil, apperr.Invalid("信源用途不正确")
	}
	if in.Trust != "official" && in.Trust != "verified" && in.Trust != "community" && in.Trust != "excluded" {
		return nil, apperr.Invalid("可信等级不正确")
	}
	if in.Kind == "json" || in.Kind == "web" || in.Kind == "x" || in.Kind == "wechat" {
		cfg, err := sourceconfig.Parse(in.Kind, in.Config, true)
		if err != nil {
			return nil, apperr.Invalid(err.Error())
		}
		return json.Marshal(cfg)
	}
	var common struct {
		Limit int `json:"initial_backfill_limit"`
	}
	if len(in.Config) > 0 && json.Unmarshal(in.Config, &common) != nil {
		return nil, apperr.Invalid("采集配置不正确")
	}
	if common.Limit == 0 {
		common.Limit = 8
	}
	if common.Limit < 1 || common.Limit > 100 {
		return nil, apperr.Invalid("首次收录上限需为 1–100 条")
	}
	config := map[string]any{"initial_backfill_limit": common.Limit}
	switch in.Kind {
	case "github":
		parts := strings.Split(strings.TrimSpace(in.Repository), "/")
		if len(parts) != 2 {
			return nil, apperr.Invalid("仓库需填写为 owner/name")
		}
		for _, part := range parts {
			if len(part) > 100 || !repoPart.MatchString(part) || part == "." || part == ".." {
				return nil, apperr.Invalid("仓库名称不正确")
			}
		}
		config["owner"] = parts[0]
		config["name"] = parts[1]
		config["mode"] = "repo"
	case "rss":
		if err := sourceconfig.PublicURL(in.FeedURL); err != nil {
			return nil, apperr.Invalid(err.Error())
		}
		config["feed_url"] = strings.TrimSpace(in.FeedURL)
	case "external":
		config = map[string]any{}
	default:
		return nil, apperr.Invalid("不支持的信源类型")
	}
	return json.Marshal(config)
}

func (s Service) SaveSourceTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in SourceInput) (uuid.UUID, int64, error) {
	config, err := in.validate()
	if err != nil {
		return uuid.Nil, 0, err
	}
	if in.Enabled && (in.Kind == "x" || in.Kind == "wechat") {
		if err := social.Ready(in.Kind); err != nil {
			return uuid.Nil, 0, err
		}
	}
	now := time.Now().UTC()
	var next *time.Time
	if in.Enabled && in.Trust != "excluded" && in.Kind != "external" {
		v := now.Add(time.Duration(in.Interval) * time.Second)
		next = &v
	}
	var version int64
	if id == uuid.Nil {
		id = uuid.New()
		err = tx.QueryRow(ctx, `INSERT INTO sources(id,source_key,name,kind,participation_mode,trust_tier,config,enabled,interval_seconds,allow_fulltext,next_fetch_at)
  VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING edit_version`, id, in.Kind+"-"+id.String()[:8], in.Name, in.Kind, in.Mode, in.Trust, config, in.Enabled, in.Interval, in.AllowFulltext, next).Scan(&version)
	} else {
		var kind string
		var existing int64
		var oldConfig json.RawMessage
		err = tx.QueryRow(ctx, `SELECT kind,edit_version,config FROM sources WHERE id=$1 FOR UPDATE`, id).Scan(&kind, &existing, &oldConfig)
		if errors.Is(err, pgx.ErrNoRows) {
			return id, 0, apperr.NotFound("信源不存在")
		}
		if err != nil {
			return id, 0, dbError(err)
		}
		if existing != in.EditVersion {
			return id, 0, apperr.EditConflict("信源已被修改，请刷新后重试")
		}
		if kind != in.Kind {
			return id, 0, apperr.Invalid("已有信源的类型不能修改")
		}
		err = tx.QueryRow(ctx, `UPDATE sources SET name=$2,participation_mode=$3,trust_tier=$4,checkpoint=CASE WHEN $10 THEN '{}'::jsonb ELSE checkpoint END,config=$5,enabled=$6,
   interval_seconds=$7,allow_fulltext=$8,next_fetch_at=$9,edit_version=edit_version+1,updated_at=now() WHERE id=$1 RETURNING edit_version`, id, in.Name, in.Mode, in.Trust, config, in.Enabled, in.Interval, in.AllowFulltext, next, checkpointConfigChanged(kind, oldConfig, config)).Scan(&version)
	}
	return id, version, dbError(err)
}

func (s Service) QueueSourceTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, expected int64) (uuid.UUID, error) {
	if s.Jobs == nil {
		return uuid.Nil, apperr.Unavailable("任务队列暂不可用")
	}
	var kind, trust string
	var enabled bool
	var version int64
	var checkpoint json.RawMessage
	err := tx.QueryRow(ctx, `SELECT kind,trust_tier,enabled,edit_version,checkpoint FROM sources WHERE id=$1 FOR UPDATE`, id).Scan(&kind, &trust, &enabled, &version, &checkpoint)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, apperr.NotFound("信源不存在")
	}
	if err != nil {
		return uuid.Nil, dbError(err)
	}
	if expected != version {
		return uuid.Nil, apperr.EditConflict("信源已被修改，请刷新后重试")
	}
	if !enabled || trust == "excluded" {
		return uuid.Nil, apperr.Invalid("请先启用信源")
	}
	if kind == "external" {
		return uuid.Nil, apperr.Invalid("此信源通过外部推送接收资料，不支持主动抓取")
	}
	if kind == "x" || kind == "wechat" {
		if err := social.Ready(kind); err != nil {
			return uuid.Nil, err
		}
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM source_runs WHERE source_id=$1 AND status IN ('pending','running'))`, id).Scan(&active); err != nil {
		return uuid.Nil, dbError(err)
	}
	if active {
		return uuid.Nil, apperr.EditConflict("已有采集任务在排队或运行")
	}
	runID := uuid.New()
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO source_runs(id,source_id,source_edit_version,scheduled_for,run_key,checkpoint_before) VALUES($1,$2,$3,$4,$5,$6)`, runID, id, version, now, "admin:"+runID.String(), checkpoint)
	if err != nil {
		return uuid.Nil, dbError(err)
	}
	job, err := s.Jobs.InsertTx(ctx, tx, ingest.FetchArgs{SourceRunID: runID}, nil)
	if err != nil {
		return uuid.Nil, dbError(err)
	}
	_, err = tx.Exec(ctx, `UPDATE source_runs SET river_job_id=$2 WHERE id=$1`, runID, job.Job.ID)
	return runID, dbError(err)
}

func dbError(err error) error {
	if err == nil {
		return nil
	}
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return err
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return apperr.EditConflict("记录已存在，请刷新后重试")
	}
	wrapped := apperr.Internal("自动化操作失败")
	wrapped.Err = err
	return wrapped
}

type Page struct {
	Items      []json.RawMessage `json:"items"`
	HasMore    bool              `json:"has_more"`
	NextOffset int               `json:"next_offset"`
}

func (s Service) Page(ctx context.Context, query string, offset int, args ...any) (Page, error) {
	rows, err := s.Pool.Query(ctx, query, args...)
	if err != nil {
		return Page{}, dbError(err)
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return Page{}, dbError(err)
		}
		items = append(items, raw)
	}
	if err := rows.Err(); err != nil {
		return Page{}, dbError(err)
	}
	more := len(items) > 50
	if more {
		items = items[:50]
	}
	return Page{Items: items, HasMore: more, NextOffset: offset + len(items)}, nil
}
func (s Service) Object(ctx context.Context, query string, args ...any) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.Pool.QueryRow(ctx, query, args...).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("记录不存在")
	}
	return raw, dbError(err)
}

const SourcesQuery = `SELECT to_jsonb(v) FROM (
 SELECT s.id,s.source_key,s.name,s.kind,s.config,s.participation_mode,s.trust_tier,s.enabled,s.interval_seconds,s.allow_fulltext,s.edit_version,s.next_fetch_at,s.last_success_at,s.failure_count,
  COALESCE(s.config->>'feed_url','') AS feed_url,
  CASE WHEN s.kind='github' THEN COALESCE(s.config->>'owner','')||'/'||COALESCE(s.config->>'name','') ELSE '' END AS repository,
  (SELECT count(*) FROM raw_item_discoveries d WHERE d.source_id=s.id) AS item_count,
  (SELECT jsonb_build_object('id',r.id,'status',r.status,'error_message',r.error_message,'created_at',r.created_at) FROM source_runs r WHERE r.source_id=s.id ORDER BY r.created_at DESC,r.id DESC LIMIT 1) AS last_run
 FROM sources s ORDER BY s.created_at DESC,s.id DESC LIMIT 51 OFFSET $1) v`

const IngestRunsQuery = `SELECT to_jsonb(v) FROM (
 SELECT r.id,r.source_id,s.name AS source_name,r.status,r.stats,r.error_code,r.error_message,r.created_at,r.started_at,r.finished_at
 FROM source_runs r JOIN sources s ON s.id=r.source_id
 WHERE ($1='' OR r.status=$1) AND ($2='' OR r.source_id::text=$2)
 ORDER BY r.created_at DESC,r.id DESC LIMIT 51 OFFSET $3) v`

const ProcessingQuery = `SELECT to_jsonb(v) FROM (
 SELECT r.id,r.raw_revision_id,r.stage,r.status,r.attempt_count,r.rerun_no,r.error_code,r.error_message,r.created_at,r.updated_at,
 v.title,v.raw_item_id,s.name AS source_name,s.id AS source_id,
 (SELECT p.id FROM change_proposals p WHERE p.processing_run_id=r.id ORDER BY p.created_at DESC LIMIT 1) AS proposal_id,
 EXISTS(SELECT 1 FROM provider_calls c WHERE c.processing_run_id=r.id AND c.status IN ('prepared','sent','unknown')) AS uncertain_call
 FROM processing_runs r JOIN raw_item_revisions v ON v.id=r.raw_revision_id JOIN raw_items i ON i.id=v.raw_item_id JOIN sources s ON s.id=i.owner_source_id
 WHERE ($1='' OR r.status=$1) AND ($2='' OR s.id::text=$2)
 ORDER BY r.created_at DESC,r.id DESC LIMIT 51 OFFSET $3) v`

const RunContextQuery = `SELECT jsonb_build_object('id',r.id,'raw_item_id',i.id,'title',v.title,'source_name',s.name,'source_url',i.canonical_url,
 'allow_fulltext',s.allow_fulltext,'body',left(COALESCE(NULLIF(v.body_text,''),NULLIF(v.excerpt,''),v.body_html,''),30000),
 'body_truncated',length(COALESCE(NULLIF(v.body_text,''),NULLIF(v.excerpt,''),v.body_html,''))>30000,
 'stages',(SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY x.created_at,x.id),'[]') FROM (SELECT id,stage,status,error_code,error_message,attempt_count,created_at FROM processing_runs WHERE raw_revision_id=r.raw_revision_id AND pipeline_key=r.pipeline_key) x))
 FROM processing_runs r JOIN raw_item_revisions v ON v.id=r.raw_revision_id JOIN raw_items i ON i.id=v.raw_item_id JOIN sources s ON s.id=i.owner_source_id WHERE r.id=$1`

const ProposalsQuery = `SELECT to_jsonb(v) FROM (
 SELECT p.id,p.resource_id,p.applied_resource_id,p.proposed_kind,p.status,p.created_at,p.updated_at,p.processing_run_id,
 COALESCE(p.proposed_payload->>'title',v.title) AS title,s.name AS source_name,
 (SELECT count(*) FROM jsonb_object_keys(p.field_changes)) AS change_count
 FROM change_proposals p JOIN processing_runs r ON r.id=p.processing_run_id JOIN raw_item_revisions v ON v.id=r.raw_revision_id JOIN raw_items i ON i.id=v.raw_item_id JOIN sources s ON s.id=i.owner_source_id
 WHERE ($1='' OR p.status=$1) AND ($2='' OR p.proposed_kind=$2) ORDER BY p.created_at DESC,p.id DESC LIMIT 51 OFFSET $3) v`

const OverviewQuery = `SELECT jsonb_build_object(
 'sources',(SELECT count(*) FROM sources), 'enabled_sources',(SELECT count(*) FROM sources WHERE enabled AND trust_tier<>'excluded'),
 'new_items_24h',(SELECT count(*) FROM raw_items WHERE first_discovered_at>now()-interval '24 hours'),
 'pending_reviews',(SELECT count(*) FROM change_proposals WHERE status='pending'),
 'blocked_stages',(SELECT count(*) FROM processing_runs r JOIN raw_items i ON i.current_revision_id=r.raw_revision_id WHERE r.status IN ('blocked','failed') AND r.rerun_no=(SELECT max(x.rerun_no) FROM processing_runs x WHERE x.raw_revision_id=r.raw_revision_id)),
 'active_fetches',(SELECT count(*) FROM source_runs WHERE status IN ('pending','running')),
 'failed_fetches',(SELECT count(*) FROM source_runs WHERE status='failed'),
 'unknown_calls',(SELECT count(*) FROM provider_calls WHERE status='unknown'),
 'budgets',(SELECT COALESCE(jsonb_agg(to_jsonb(b)),'[]') FROM (SELECT scope_key,currency,limit_amount::text,reserved_amount::text,spent_amount::text,window_start,window_end FROM provider_budget_windows WHERE window_start<=now() AND window_end>now() AND scope_key NOT LIKE 'task:%' ORDER BY scope_key LIMIT 20) b))`

const CallsQuery = `SELECT to_jsonb(v) FROM (
 SELECT id,processing_run_id,source_run_id,assistant_turn_id,request_summary,provider_key,model,status,input_tokens,output_tokens,currency,reserved_cost::text,actual_cost::text,error_code,created_at,completed_at
 FROM provider_calls WHERE ($1='' OR status=$1) ORDER BY created_at DESC,id DESC LIMIT 51 OFFSET $2) v`

// PublicConfig returns only implemented, non-credential configuration fields.
func PublicConfig(kind string, raw json.RawMessage) json.RawMessage {
	if kind == "github" || kind == "rss" {
		var c struct {
			Limit int `json:"initial_backfill_limit"`
		}
		_ = json.Unmarshal(raw, &c)
		if c.Limit == 0 {
			c.Limit = 8
		}
		out, _ := json.Marshal(map[string]int{"initial_backfill_limit": c.Limit})
		return out
	}
	if kind == "external" {
		return json.RawMessage(`{}`)
	}
	return sourceconfig.Public(kind, raw)
}
func (in *SourceInput) PreviewConfig() (json.RawMessage, error) {
	copy := *in
	if strings.TrimSpace(copy.Name) == "" {
		copy.Name = "试抓"
	}
	if copy.Interval == 0 {
		copy.Interval = 3600
	}
	if copy.Mode == "" {
		copy.Mode = "internal"
	}
	if copy.Trust == "" {
		copy.Trust = "community"
	}
	return copy.validate()
}

func checkpointConfigChanged(kind string, before, after json.RawMessage) bool {
	normalize := func(raw json.RawMessage) []byte {
		var obj map[string]any
		_ = json.Unmarshal(raw, &obj)
		if obj == nil {
			obj = map[string]any{}
		}
		if _, ok := obj["initial_backfill_limit"]; !ok {
			obj["initial_backfill_limit"] = 8
		}
		if kind == "wechat" {
			delete(obj, "nickname")
		}
		out, _ := json.Marshal(obj)
		return out
	}
	return !bytes.Equal(normalize(before), normalize(after))
}
