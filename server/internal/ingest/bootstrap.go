package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// First-time imports keep only the configured newest items. Hashes of deliberately skipped items
// survive checkpoints, so the next poll does not accidentally import the rest of the back catalogue.
func bootstrapItems(src Source, batch *FetchBatch) int {
	var cfg struct {
		Limit int `json:"initial_backfill_limit"`
	}
	if json.Unmarshal(src.Config, &cfg) != nil || cfg.Limit < 1 {
		return 0
	}
	var before, after map[string]json.RawMessage
	_ = json.Unmarshal(src.Checkpoint, &before)
	_ = json.Unmarshal(batch.Checkpoint, &after)
	if after == nil {
		after = map[string]json.RawMessage{}
	}
	var state struct {
		Initialized bool              `json:"initialized"`
		Ignored     map[string]string `json:"ignored"`
	}
	_ = json.Unmarshal(before["_nex_bootstrap"], &state)
	if _, exists := before["_nex_bootstrap"]; !exists && len(before) > 0 {
		state.Initialized = true
	}
	if state.Ignored == nil {
		state.Ignored = map[string]string{}
	}
	if !state.Initialized {
		sort.SliceStable(batch.Items, func(i, j int) bool {
			a, b := batch.Items[i].PublishedAt, batch.Items[j].PublishedAt
			return a != nil && (b == nil || a.After(*b))
		})
	}
	out := []IncomingItem{}
	skipped := 0
	for _, item := range batch.Items {
		identity, err := ResolveIdentity(src.ID, src.Key, item)
		if err != nil {
			out = append(out, item)
			continue
		}
		sum := sha256.Sum256([]byte(identity.Key))
		key := hex.EncodeToString(sum[:])
		hash, err := ItemContentHash(item)
		if err != nil {
			out = append(out, item)
			continue
		}
		if !state.Initialized && len(out) >= cfg.Limit {
			state.Ignored[key] = hash
			skipped++
			continue
		}
		if old, ok := state.Ignored[key]; ok {
			if old == hash {
				skipped++
				continue
			}
			delete(state.Ignored, key)
		}
		out = append(out, item)
	}
	state.Initialized = true
	body, _ := json.Marshal(state)
	after["_nex_bootstrap"] = body
	checkpoint, _ := json.Marshal(after)
	batch.Checkpoint = checkpoint
	batch.Items = out
	return skipped
}
