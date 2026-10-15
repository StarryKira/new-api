package oairesponses

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func responsesReasoningText(item map[string]any) string {
	for _, key := range []string{"summary", "content"} {
		parts, _ := responsesContentParts(item[key])
		var text strings.Builder
		for _, part := range parts {
			text.WriteString(kitutil.Interface2String(part["text"]))
		}
		if text.Len() > 0 {
			return text.String()
		}
	}
	return kitutil.Interface2String(item["text"])
}

func responsesAgentMessageText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	parts, _ := responsesContentParts(content)
	var text strings.Builder
	for _, part := range parts {
		switch kitutil.Interface2String(part["type"]) {
		case "input_text", "output_text", "text":
			text.WriteString(kitutil.Interface2String(part["text"]))
		case "encrypted_content":
			text.WriteString(kitutil.Interface2String(part["encrypted_content"]))
		}
	}
	return text.String()
}
