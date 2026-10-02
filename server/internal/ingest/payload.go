package ingest

import (
	"encoding/json"
	"strings"
)

// ScrubPayload drops Authorization keys and rejects non-objects.
func ScrubPayload(raw json.RawMessage) json.RawMessage {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return json.RawMessage(`{}`)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return json.RawMessage(`{}`)
	}
	obj, ok := v.(map[string]any)
	if !ok || obj == nil {
		return json.RawMessage(`{}`)
	}
	scrubMap(obj)
	out, err := json.Marshal(obj)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}

func scrubMap(m map[string]any) {
	for key, val := range m {
		if strings.EqualFold(key, "authorization") {
			delete(m, key)
			continue
		}
		if child, ok := val.(map[string]any); ok {
			scrubMap(child)
		}
	}
}

func decoratePayload(raw json.RawMessage, truncated bool) json.RawMessage {
	payload := ScrubPayload(raw)
	if !truncated {
		return payload
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil || obj == nil {
		obj = map[string]any{}
	}
	obj["truncated"] = true
	out, err := json.Marshal(obj)
	if err != nil {
		return json.RawMessage(`{"truncated":true}`)
	}
	return out
}
