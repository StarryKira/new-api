package claude

import (
	"encoding/base64"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// ThinkingEnvelopePrefix identifies opaque Claude replay state carried through
// Responses encrypted_content. It is not an OpenAI encrypted reasoning token.
const ThinkingEnvelopePrefix = "anthropic-thinking-v1:"

func EncodeThinkingBlock(block dto.ClaudeMediaMessage) (string, error) {
	thinking := ""
	if block.Thinking != nil {
		thinking = *block.Thinking
	}
	payload, err := kitutil.Marshal(struct {
		Type      string `json:"type"`
		Thinking  string `json:"thinking"`
		Signature string `json:"signature,omitempty"`
		Data      string `json:"data,omitempty"`
	}{block.Type, thinking, block.Signature, block.Data})
	if err != nil {
		return "", err
	}
	return ThinkingEnvelopePrefix + base64.RawStdEncoding.EncodeToString(payload), nil
}

func DecodeThinkingBlock(encoded string) (dto.ClaudeMediaMessage, bool) {
	var block dto.ClaudeMediaMessage
	if !strings.HasPrefix(encoded, ThinkingEnvelopePrefix) {
		return block, false
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, ThinkingEnvelopePrefix))
	if err != nil || kitutil.Unmarshal(raw, &block) != nil {
		return dto.ClaudeMediaMessage{}, false
	}
	if block.Type != "thinking" && block.Type != "redacted_thinking" {
		return dto.ClaudeMediaMessage{}, false
	}
	if block.Type == "thinking" && block.Signature == "" || block.Type == "redacted_thinking" && block.Data == "" {
		return dto.ClaudeMediaMessage{}, false
	}
	if block.Type == "redacted_thinking" {
		block.Thinking = nil
	}
	return block, true
}
