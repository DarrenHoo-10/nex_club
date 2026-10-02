package automation

import (
	"context"
	"encoding/json"
	"github.com/darrenhoo/nex_club/server/internal/platform/apperr"
	"github.com/darrenhoo/nex_club/server/internal/sources/presets"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PresetView struct {
	presets.Source
	Imported bool `json:"imported"`
}

func (s Service) Presets(ctx context.Context) ([]PresetView, error) {
	all, err := presets.All()
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT source_key,COALESCE(config->>'feed_url','') FROM sources WHERE kind='rss'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := map[string]bool{}
	urls := map[string]bool{}
	for rows.Next() {
		var key, url string
		if err := rows.Scan(&key, &url); err != nil {
			return nil, err
		}
		keys[key] = true
		urls[url] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []PresetView{}
	for _, p := range all {
		out = append(out, PresetView{Source: p, Imported: keys["aihot:"+p.ID] || urls[p.FeedURL]})
	}
	return out, nil
}

type ImportResult struct {
	Created  int         `json:"created"`
	Existing int         `json:"existing"`
	IDs      []uuid.UUID `json:"source_ids"`
}

func (s Service) ImportPresetsTx(ctx context.Context, tx pgx.Tx, ids []string) (ImportResult, error) {
	result := ImportResult{IDs: []uuid.UUID{}}
	all, err := presets.All()
	if err != nil {
		return result, err
	}
	known := map[string]presets.Source{}
	for _, p := range all {
		known[p.ID] = p
	}
	if len(ids) == 0 || len(ids) > len(all) {
		return result, apperr.Invalid("请选择要导入的示范信源")
	}
	selected := map[string]bool{}
	for _, id := range ids {
		if _, ok := known[id]; !ok {
			return result, apperr.Invalid("示范信源不存在")
		}
		selected[id] = true
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('nex:source-presets',0))`); err != nil {
		return result, err
	}
	for _, p := range all {
		if !selected[p.ID] {
			continue
		}
		key := "aihot:" + p.ID
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sources WHERE source_key=$1 OR (kind='rss' AND config->>'feed_url'=$2))`, key, p.FeedURL).Scan(&exists); err != nil {
			return result, err
		}
		if exists {
			result.Existing++
			continue
		}
		raw, _ := json.Marshal(map[string]any{"initial_backfill_limit": p.InitialLimit})
		cfg, err := (&SourceInput{Name: p.Name, Kind: "rss", FeedURL: p.FeedURL, Mode: p.Mode, Trust: p.Trust, Interval: p.Interval, Config: raw}).validate()
		if err != nil {
			return result, err
		}
		id := uuid.New()
		_, err = tx.Exec(ctx, `INSERT INTO sources(id,source_key,name,kind,participation_mode,trust_tier,config,enabled,interval_seconds,allow_fulltext) VALUES($1,$2,$3,'rss',$4,$5,$6,false,$7,false)`, id, key, p.Name, p.Mode, p.Trust, cfg, p.Interval)
		if err != nil {
			return result, err
		}
		result.Created++
		result.IDs = append(result.IDs, id)
	}
	return result, nil
}
