// Adapted from StarryKira/sub2api's Grok compaction compatibility bridge.
// Original code is licensed under LGPL-3.0; see LGPL-3.0.txt.
package responsescompat

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// RestoreGrokCompactionInput reverses compact output items from prior
// turns. The encrypted blob originated as Grok reasoning and must be replayed
// under that type. The visible summary is added as conversation context.
func RestoreGrokCompactionInput(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := kitutil.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	items, ok := payload["input"].([]any)
	if !ok {
		return body, nil
	}

	changed := false
	converted := make([]any, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || !IsCompactionType(stringValue(item["type"])) {
			converted = append(converted, raw)
			continue
		}
		changed = true
		if encrypted := strings.TrimSpace(stringValue(item["encrypted_content"])); encrypted != "" {
			converted = append(converted, map[string]any{
				"type":              "reasoning",
				"summary":           []any{},
				"encrypted_content": encrypted,
			})
		}
		if summary := compactSummaryText(item["summary"]); summary != "" {
			converted = append(converted, map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{map[string]any{
					"type": "input_text",
					"text": "<conversation_summary>\n" + summary + "\n</conversation_summary>",
				}},
			})
		}
	}
	if !changed {
		return body, nil
	}
	payload["input"] = converted
	encoded, err := kitutil.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func GrokResponseToCompaction(body []byte, itemID string) ([]byte, error) {
	var response map[string]any
	if err := kitutil.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	output, ok := response["output"].([]any)
	if !ok {
		return nil, fmt.Errorf("response has no output array")
	}

	var encrypted string
	var summaryParts []string
	for _, raw := range output {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch strings.TrimSpace(stringValue(item["type"])) {
		case "reasoning":
			if value := strings.TrimSpace(stringValue(item["encrypted_content"])); value != "" {
				encrypted = value
			}
		case "message":
			if content, ok := item["content"].([]any); ok {
				for _, rawContent := range content {
					part, ok := rawContent.(map[string]any)
					if !ok {
						continue
					}
					if text := strings.TrimSpace(stringValue(part["text"])); text != "" {
						summaryParts = append(summaryParts, text)
					}
				}
			}
		}
	}
	if encrypted == "" {
		return nil, fmt.Errorf("response has no reasoning.encrypted_content")
	}

	compactItem := map[string]any{
		"id":                itemID,
		"type":              "compaction",
		"status":            "completed",
		"encrypted_content": encrypted,
	}
	if summary := strings.TrimSpace(strings.Join(summaryParts, "\n")); summary != "" {
		compactItem["summary"] = []any{map[string]any{
			"type": "summary_text",
			"text": summary,
		}}
	}
	response["output"] = []any{compactItem}
	response["status"] = "completed"
	delete(response, "output_text")

	encoded, err := kitutil.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode compact response: %w", err)
	}
	return encoded, nil
}

func compactSummaryText(value any) string {
	parts, ok := value.([]any)
	if !ok {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if text := strings.TrimSpace(stringValue(part["text"])); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "\n")
}

func IsCompactionType(value string) bool {
	switch strings.TrimSpace(value) {
	case "compaction", "compaction_summary":
		return true
	default:
		return false
	}
}

// NormalizeInputItems accepts the native Responses input shorthand.
func NormalizeInputItems(value any) ([]any, error) {
	switch input := value.(type) {
	case nil:
		return []any{}, nil
	case []any:
		return input, nil
	case string:
		return []any{map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{map[string]any{
				"type": "input_text",
				"text": input,
			}},
		}}, nil
	case map[string]any:
		return []any{input}, nil
	default:
		return nil, fmt.Errorf("compact input must be a string, object, or array")
	}
}
