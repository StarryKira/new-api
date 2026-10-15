package relayconvert

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

// normalizeChatInstructions preserves the Responses-compatible instructions
// extension accepted by Chat bridges. Convert it once to a system message so
// every target reuses its existing instruction mapping without mutating input.
func normalizeChatInstructions(request any) any {
	var source *dto.GeneralOpenAIRequest
	switch value := request.(type) {
	case *dto.GeneralOpenAIRequest:
		source = value
	case dto.GeneralOpenAIRequest:
		source = &value
	default:
		return request
	}
	if source == nil || source.Instructions == nil || strings.TrimSpace(*source.Instructions) == "" {
		return request
	}
	clone := *source
	clone.Messages = make([]dto.Message, 0, len(source.Messages)+1)
	clone.Messages = append(clone.Messages, dto.Message{Role: "system", Content: *source.Instructions})
	clone.Messages = append(clone.Messages, source.Messages...)
	clone.Instructions = nil
	return &clone
}
