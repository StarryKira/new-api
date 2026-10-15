package dto

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// NormalizeClaudeContentBlockJSON supplies required empty fields without
// reparsing arbitrary tool input, citations or future block fields into DTOs.
func NormalizeClaudeContentBlockJSON(raw []byte) ([]byte, error) {
	var block map[string]json.RawMessage
	if err := kitutil.Unmarshal(raw, &block); err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("Claude content block must be an object")
	}
	// ClaudeMediaMessage also represents untagged message/stop-reason deltas.
	if len(block["type"]) == 0 {
		return raw, nil
	}
	var kind string
	if err := kitutil.Unmarshal(block["type"], &kind); err != nil {
		return nil, err
	}
	fields := []string(nil)
	switch kind {
	case "text":
		fields = []string{"text"}
	case "thinking":
		fields = []string{"thinking", "signature"}
	}
	changed := false
	for _, key := range fields {
		if value := block[key]; len(value) == 0 || string(value) == "null" {
			block[key] = json.RawMessage(`""`)
			changed = true
		}
	}
	if !changed {
		return raw, nil
	}
	return kitutil.Marshal(block)
}
