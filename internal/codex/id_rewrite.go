package codex

import (
	"encoding/json"
	"strconv"
)

// rewriteID returns a copy of the JSON-RPC message with its id field replaced by newID.
func rewriteID(payload []byte, newID int64) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	raw["id"] = json.RawMessage(strconv.FormatInt(newID, 10))
	return json.Marshal(raw)
}

// restoreID returns a copy of the JSON-RPC message with its id field replaced by origID.
func restoreID(payload []byte, origID json.RawMessage) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	raw["id"] = origID
	return json.Marshal(raw)
}

func idAsInt64(raw json.RawMessage) (int64, bool) {
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int64(f), true
	}
	return 0, false
}
