// Package presets embeds AIHOT's public RSS examples; imports never overwrite user configuration.
package presets

import (
	_ "embed"
	"encoding/json"
)

const UpstreamCommit = "ddf1c19ef2302863748dce51e4fdcd60d4415fc0"
const UpstreamURL = "https://github.com/KKKKhazix/AIHOT/blob/" + UpstreamCommit + "/industry/sources.json"

//go:embed aihot.json
var data []byte

type Source struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	FeedURL      string `json:"feed_url"`
	Interval     int    `json:"interval_seconds"`
	Trust        string `json:"trust_tier"`
	Mode         string `json:"participation_mode"`
	InitialLimit int    `json:"initial_backfill_limit"`
}

func All() ([]Source, error) {
	var doc struct {
		Sources []struct {
			ID, Name, Kind, Tier string
			Config               struct {
				FeedURL string `json:"feedUrl"`
				AIHOT   struct {
					Limit int `json:"initialBackfillLimit"`
				} `json:"_aihot"`
			}
			Interval int `json:"interval_minutes"`
		} `json:"sources"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := []Source{}
	for _, s := range doc.Sources {
		if s.Kind != "rss" {
			continue
		}
		trust := "community"
		if s.Tier == "T1" {
			trust = "official"
		} else if s.Tier == "T1_5" {
			trust = "verified"
		}
		out = append(out, Source{ID: s.ID, Name: s.Name, Kind: s.Kind, FeedURL: s.Config.FeedURL, Interval: s.Interval * 60, Trust: trust, Mode: "content", InitialLimit: s.Config.AIHOT.Limit})
	}
	return out, nil
}
