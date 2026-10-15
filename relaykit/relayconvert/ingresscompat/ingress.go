package ingresscompat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// NormalizeLegacyIngress accepts unambiguous Chat-shaped Responses aliases.
// Native input always wins, and unknown fields keep their original JSON values.
func NormalizeLegacyIngress(ctx context.Context, meta *convmeta.Values, body []byte) ([]byte, bool, error) {
	if len(body) == 0 {
		return body, false, nil
	}
	var request map[string]json.RawMessage
	if err := kitutil.Unmarshal(body, &request); err != nil {
		return body, false, fmt.Errorf("normalize legacy Responses ingress: %w", err)
	}
	if request == nil {
		return body, false, fmt.Errorf("Responses request must be an object")
	}
	changed := false
	if messages, exists := request["messages"]; exists {
		var items []json.RawMessage
		if kitutil.Unmarshal(messages, &items) == nil && len(items) > 0 && !hasNativeInput(request) {
			var chat dto.GeneralOpenAIRequest
			if err := kitutil.Unmarshal(body, &chat); err != nil {
				return body, false, err
			}
			converted, err := relayconvert.ConvertRequest(ctx, meta, types.RelayFormatOpenAIResponses, &chat)
			if err != nil {
				return body, false, err
			}
			encoded, err := kitutil.Marshal(converted.Value)
			if err != nil {
				return body, false, err
			}
			var fields map[string]json.RawMessage
			if err := kitutil.Unmarshal(encoded, &fields); err != nil {
				return body, false, err
			}
			request["input"] = fields["input"]
			if rawText(request["instructions"]) == "" && rawText(fields["instructions"]) != "" {
				request["instructions"] = fields["instructions"]
			}
			for _, key := range []string{"max_output_tokens", "temperature", "top_p", "service_tier"} {
				if value, ok := fields[key]; ok {
					request[key] = value
				}
			}
			if chatStyleTools(request) || request["functions"] != nil {
				copyConvertedField(request, fields, "tools")
			}
			var choice map[string]json.RawMessage
			_ = kitutil.Unmarshal(request["tool_choice"], &choice)
			if choice["function"] != nil || request["function_call"] != nil {
				copyConvertedField(request, fields, "tool_choice")
			}
			if rawText(request["reasoning_effort"]) != "" {
				copyConvertedField(request, fields, "reasoning")
			}
			for _, key := range []string{"previous_response_id", "max_tokens", "max_completion_tokens", "reasoning_effort", "functions", "function_call"} {
				delete(request, key)
			}
		}
		delete(request, "messages")
		changed = true
	}
	if prompt, exists := request["prompt"]; exists {
		var text string
		if kitutil.Unmarshal(prompt, &text) == nil && string(prompt) != "null" {
			if !hasNativeInput(request) {
				request["input"] = prompt
			}
			delete(request, "prompt")
			changed = true
		}
	}
	if _, exists := request["commands"]; exists {
		delete(request, "commands")
		changed = true
	}
	if !changed {
		return body, false, nil
	}
	normalized, err := kitutil.Marshal(request)
	return normalized, true, err
}

func hasNativeInput(request map[string]json.RawMessage) bool {
	input := strings.TrimSpace(string(request["input"]))
	return input != "" && input != "null"
}

func chatStyleTools(request map[string]json.RawMessage) bool {
	var tools []map[string]json.RawMessage
	if kitutil.Unmarshal(request["tools"], &tools) != nil {
		return false
	}
	for _, tool := range tools {
		if tool["function"] != nil {
			return true
		}
	}
	return false
}

func copyConvertedField(target, source map[string]json.RawMessage, key string) {
	if value, exists := source[key]; exists {
		target[key] = value
	}
}

func rawText(raw json.RawMessage) string {
	var text string
	_ = kitutil.Unmarshal(raw, &text)
	return strings.TrimSpace(text)
}
