package geminichat

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// ResponseGeminiChat2Claude preserves the native ordering of Gemini content
// parts. Converting the whole response through a single Chat message would move
// every text part before every tool call and coalesce independent signatures.
// The existing codecs still define each part's representation and stop/usage
// semantics; only the content sequence is assembled here.
func ResponseGeminiChat2Claude(id string, created int64, response *dto.GeminiChatResponse, info convmeta.Meta) *dto.ClaudeResponse {
	result := oaichat.ResponseOpenAI2Claude(ResponseGeminiChat2OpenAI(id, created, response), info)
	var content []dto.ClaudeMediaMessage
	for _, candidate := range response.Candidates {
		for index, part := range candidate.Content.Parts {
			isolated := candidate
			isolated.Content.Parts = []dto.GeminiPart{part}
			isolated.GroundingMetadata = groundingForPart(candidate, index)
			chat := ResponseGeminiChat2OpenAI(id, created, &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{isolated}})
			blocks := oaichat.ResponseOpenAI2Claude(chat, info).Content
			for _, block := range blocks {
				// A signature-only part is already represented by its thinking
				// block; an empty fallback text block adds no source content.
				if block.Type == "text" && block.GetText() == "" && len(blocks) > 1 {
					continue
				}
				content = append(content, block)
			}
		}
	}
	if len(content) > 0 {
		result.Content = content
	}
	return result
}

// Grounding segment offsets are local to the source part. Retain only supports
// for this part and rebase its index for the existing single-part renderer.
func groundingForPart(candidate dto.GeminiChatCandidate, index int) *dto.GeminiGroundingMetadata {
	metadata := candidate.GroundingMetadata
	if metadata == nil {
		return nil
	}
	var supports []geminiGroundingSupport
	if err := kitutil.Unmarshal(metadata.GroundingSupports, &supports); err != nil {
		return nil
	}
	textParts := 0
	for _, part := range candidate.Content.Parts {
		if part.Text != "" && !part.Thought {
			textParts++
		}
	}
	selected := make([]geminiGroundingSupport, 0)
	for _, support := range supports {
		if support.Segment.PartIndex != nil {
			if *support.Segment.PartIndex != index {
				continue
			}
		} else if textParts != 1 {
			continue
		}
		support.Segment.PartIndex = kitutil.GetPointer(0)
		selected = append(selected, support)
	}
	if len(selected) == 0 {
		return nil
	}
	copy := *metadata
	copy.GroundingSupports, _ = kitutil.Marshal(selected)
	return &copy
}
