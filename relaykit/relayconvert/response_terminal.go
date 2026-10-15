package relayconvert

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

// ErrIncompleteResponseStream reports a stream that ended without an upstream
// completion signal. Already collected usage remains available on its state.
var ErrIncompleteResponseStream = errors.New("upstream stream ended without a terminal event")

// MarkTransportComplete records an explicit transport terminator, such as the
// OpenAI SSE [DONE] marker. Callers must not use it for an ordinary EOF.
// It also permits an empty, explicitly terminated stream to be finalized.
func (s *ResponseStreamState) MarkTransportComplete() {
	if s != nil {
		s.transportComplete = true
	}
}

func sourceStreamHasTerminal(response any) bool {
	switch value := response.(type) {
	case dto.ClaudeResponse:
		return sourceStreamHasTerminal(&value)
	case dto.ChatCompletionsStreamResponse:
		return sourceStreamHasTerminal(&value)
	case dto.ResponsesStreamResponse:
		return sourceStreamHasTerminal(&value)
	case dto.GeminiChatResponse:
		return sourceStreamHasTerminal(&value)
	case *dto.ClaudeResponse:
		return value.Type == "message_stop" || (value.Type == "message_delta" && value.Delta != nil && value.Delta.StopReason != nil && strings.TrimSpace(*value.Delta.StopReason) != "")
	case *dto.ChatCompletionsStreamResponse:
		for _, choice := range value.Choices {
			if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
				return true
			}
		}
	case *dto.ResponsesStreamResponse:
		switch value.Type {
		case "response.completed", "response.done", "response.incomplete", "response.failed", "response.cancelled", "response.canceled":
			return true
		}
	case *dto.GeminiChatResponse:
		if len(value.Candidates) == 0 && value.PromptFeedback != nil && value.PromptFeedback.BlockReason != nil && strings.TrimSpace(*value.PromptFeedback.BlockReason) != "" {
			return true
		}
		for _, candidate := range value.Candidates {
			if candidate.FinishReason != nil && strings.TrimSpace(*candidate.FinishReason) != "" {
				return true
			}
		}
	}
	return false
}
