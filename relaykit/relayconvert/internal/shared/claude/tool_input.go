package claude

import "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"

// ClaudeCodeReadInput applies the opt-in Claude Code optional-pages policy.
func ClaudeCodeReadInput(name, raw string) string {
	if name != "Read" || raw == "" {
		return raw
	}
	var input map[string]any
	if kitutil.Unmarshal([]byte(raw), &input) != nil {
		return raw
	}
	if pages, ok := input["pages"].(string); !ok || pages != "" {
		return raw
	}
	delete(input, "pages")
	result, err := kitutil.Marshal(input)
	if err != nil {
		return raw
	}
	return string(result)
}
