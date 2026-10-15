package relayconvert

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// MarshalResponse serializes a conversion result for the downstream API.
// Result.Usage remains the settlement contract, including its upstream billing
// snapshot. Internal billing metadata must not be sent as protocol usage fields.
// Stream event envelopes are unwrapped to their actual JSON payload.
func MarshalResponse(result ResponseResult) ([]byte, error) {
	value := result.Value
	switch event := value.(type) {
	case ChatToResponsesStreamEvent:
		value = event.Payload
	case *ChatToResponsesStreamEvent:
		if event == nil {
			return nil, fmt.Errorf("nil Responses stream event")
		}
		value = event.Payload
	}
	encoded, err := kitutil.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := kitutil.Unmarshal(encoded, &object); err != nil {
		return nil, fmt.Errorf("response payload must be an object: %w", err)
	}
	if object == nil {
		return nil, fmt.Errorf("response payload must not be null")
	}
	if err := sanitizeWireUsage(object, result.To); err != nil {
		return nil, err
	}
	// These are the only stream envelopes containing a response's usage. Do
	// not recursively scrub arbitrary tool arguments or user-generated content.
	for _, key := range []string{"message", "response"} {
		if raw := object[key]; len(raw) > 0 && string(raw) != "null" {
			var nested map[string]json.RawMessage
			if err := kitutil.Unmarshal(raw, &nested); err != nil {
				return nil, err
			}
			if err := sanitizeWireUsage(nested, result.To); err != nil {
				return nil, err
			}
			object[key], err = kitutil.Marshal(nested)
			if err != nil {
				return nil, err
			}
		}
	}
	encoded, err = kitutil.Marshal(object)
	if err != nil {
		return nil, err
	}
	if result.To == types.RelayFormatOpenAIResponses && object["type"] != nil {
		return NormalizeResponsesStreamEventJSON(encoded)
	}
	return encoded, nil
}

func sanitizeWireUsage(object map[string]json.RawMessage, format types.RelayFormat) error {
	key := "usage"
	if format == types.RelayFormatGemini {
		key = "usageMetadata"
	}
	raw := object[key]
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if format == types.RelayFormatOpenAIResponses {
		encoded, err := FormatResponsesUsageJSON(raw)
		if err != nil {
			return err
		}
		object[key] = encoded
		return nil
	}
	var usage map[string]json.RawMessage
	if err := kitutil.Unmarshal(raw, &usage); err != nil {
		return err
	}
	for _, field := range []string{"billing_usage", "usage_semantic", "usage_source", "claude_cache_creation_5_m_tokens", "claude_cache_creation_1_h_tokens"} {
		delete(usage, field)
	}
	switch format {
	case types.RelayFormatOpenAI:
		for _, field := range []string{"input_tokens", "output_tokens", "input_tokens_details", "output_tokens_details"} {
			delete(usage, field)
		}
	case types.RelayFormatOpenAIResponses:
		for _, field := range []string{"prompt_tokens", "completion_tokens", "prompt_tokens_details", "completion_tokens_details", "prompt_cache_hit_tokens"} {
			delete(usage, field)
		}
	}
	data, err := kitutil.Marshal(usage)
	if err != nil {
		return err
	}
	object[key] = data
	return nil
}

// FormatResponsesUsageJSON accepts legacy upstream counters but emits the
// canonical Responses representation. Billing snapshots remain out of band.
func FormatResponsesUsageJSON(data []byte) ([]byte, error) {
	normalized, err := dto.NormalizeResponsesUsageJSON(data)
	if err != nil {
		return nil, err
	}
	var usage map[string]json.RawMessage
	if err := kitutil.Unmarshal(normalized, &usage); err != nil {
		return nil, err
	}
	if usage == nil {
		return normalized, nil
	}
	for _, key := range []string{
		"billing_usage", "usage_semantic", "usage_source", "claude_cache_creation_5_m_tokens", "claude_cache_creation_1_h_tokens",
		"prompt_tokens", "completion_tokens", "prompt_tokens_details", "completion_tokens_details", "prompt_cache_hit_tokens", "prompt_cache_miss_tokens",
		"cache_creation_input_tokens", "cache_write_input_tokens", "cache_creation_tokens", "cache_write_tokens",
	} {
		delete(usage, key)
	}
	return kitutil.Marshal(usage)
}
