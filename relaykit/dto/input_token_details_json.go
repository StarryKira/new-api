package dto

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// NormalizeInputTokenDetailsJSON preserves extension fields while accepting
// cache_creation_tokens as an alias of the canonical cached_creation_tokens.
// An explicitly supplied canonical value, including zero, takes precedence.
func NormalizeInputTokenDetailsJSON(data []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := kitutil.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return data, nil
	}
	if _, exists := fields["cached_creation_tokens"]; !exists {
		if alias, ok := fields["cache_creation_tokens"]; ok {
			fields["cached_creation_tokens"] = alias
		}
	}
	delete(fields, "cache_creation_tokens")
	return kitutil.Marshal(fields)
}
