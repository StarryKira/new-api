package dto

import "encoding/json"

// ExtraContent preserves provider metadata on OpenAI-compatible tool calls.
// Gemini returns signatures here so clients can replay the signed call.
type ExtraContent struct {
	Google *GoogleExtraContent `json:"google,omitempty"`
}

type GoogleExtraContent struct {
	ThoughtSignature json.RawMessage `json:"thought_signature,omitempty"`
}

func GoogleThoughtSignature(signature json.RawMessage) *ExtraContent {
	if len(signature) == 0 || string(signature) == "null" || string(signature) == `""` {
		return nil
	}
	return &ExtraContent{Google: &GoogleExtraContent{ThoughtSignature: append(json.RawMessage(nil), signature...)}}
}

func (e *ExtraContent) ThoughtSignature() json.RawMessage {
	if e == nil || e.Google == nil {
		return nil
	}
	return append(json.RawMessage(nil), e.Google.ThoughtSignature...)
}
