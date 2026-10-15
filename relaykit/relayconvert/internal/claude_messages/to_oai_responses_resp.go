package claudemessages

import (
	"fmt"
	"sort"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// ClaudeResponseToResponses preserves ordered, signed content blocks. A single
// Chat message cannot represent multiple separately signed thinking blocks.
func ClaudeResponseToResponses(response *dto.ClaudeResponse, tools *convmeta.ResponsesToolState) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	if response == nil {
		return nil, nil, fmt.Errorf("response is nil")
	}
	if response.Id == "" {
		copy := *response
		copy.Id = "resp_" + kitutil.GetUUID()
		response = &copy
	}
	chat := ResponseClaude2OpenAI(response)
	chat.Usage = *UsageFromClaudeAPIUsage(response.Usage)
	result, usage, err := oaichat.ChatCompletionsResponseToResponsesResponseWithTools(chat, response.Id, tools)
	if err != nil {
		return nil, nil, err
	}
	result.Output = make([]dto.ResponsesOutput, 0, len(response.Content))
	for index, block := range response.Content {
		id := fmt.Sprintf("%s_item_%d", response.Id, index)
		if block.Type == "thinking" || block.Type == "redacted_thinking" {
			item := dto.ResponsesOutput{Type: "reasoning", ID: id, Status: "completed"}
			if block.Thinking != nil && *block.Thinking != "" {
				item.Summary = []dto.ResponsesReasoningSummaryPart{{Type: "summary_text", Text: *block.Thinking}}
			}
			if block.Signature != "" || block.Data != "" {
				item.EncryptedContent, err = sharedclaude.EncodeThinkingBlock(block)
				if err != nil {
					return nil, nil, err
				}
			}
			if len(item.Summary) > 0 || item.EncryptedContent != "" {
				result.Output = append(result.Output, item)
			}
			continue
		}
		one := *response
		one.Content = []dto.ClaudeMediaMessage{block}
		converted, _, err := oaichat.ChatCompletionsResponseToResponsesResponseWithTools(ResponseClaude2OpenAI(&one), id, tools)
		if err != nil {
			return nil, nil, err
		}
		result.Output = append(result.Output, converted.Output...)
	}
	return result, usage, nil
}

type ClaudeToResponsesStreamState struct {
	NativeUsage *dto.ClaudeUsage
	Chat        *oaichat.ChatToResponsesStreamState
	chat        *ClaudeToChatStreamState
	hosted      *ClaudeHostedStreamBridge
	thinking    map[int]*dto.ClaudeMediaMessage
}

func NewClaudeToResponsesStreamState(id, model string) *ClaudeToResponsesStreamState {
	return &ClaudeToResponsesStreamState{Chat: oaichat.NewChatToResponsesStreamState(id, model), chat: NewClaudeToChatStreamState(), hosted: NewClaudeHostedStreamBridge(), thinking: make(map[int]*dto.ClaudeMediaMessage)}
}

func (s *ClaudeToResponsesStreamState) StreamUsage() *dto.Usage         { return s.Chat.StreamUsage() }
func (s *ClaudeToResponsesStreamState) SetStreamUsage(usage *dto.Usage) { s.Chat.SetStreamUsage(usage) }
func (s *ClaudeToResponsesStreamState) UsageText() string               { return s.Chat.UsageText() }

func (s *ClaudeToResponsesStreamState) ConvertChunk(response *dto.ClaudeResponse) ([]oaichat.ChatToResponsesStreamEvent, error) {
	if response == nil {
		return nil, nil
	}
	var events []oaichat.ChatToResponsesStreamEvent
	if response.Type == "message_delta" || response.Type == "message_stop" {
		closed, err := s.CloseThinking()
		if err != nil {
			return nil, err
		}
		events = append(events, closed...)
	}
	if response.Type == "message_start" && response.Message != nil {
		if s.Chat.ID == "" && response.Message.Id != "" {
			s.Chat.ID = response.Message.Id
		}
		if s.Chat.Model == "" && response.Message.Model != "" {
			s.Chat.Model = response.Message.Model
		}
	}
	index := response.GetIndex()
	switch response.Type {
	case "content_block_start":
		events = append(events, s.Chat.CloseContent()...)
		if response.ContentBlock != nil && (response.ContentBlock.Type == "thinking" || response.ContentBlock.Type == "redacted_thinking") {
			block := *response.ContentBlock
			s.thinking[index] = &block
			text := ""
			if block.Thinking != nil {
				text = *block.Thinking
			}
			return append(events, s.Chat.AppendReasoning(text)...), nil
		}
	case "content_block_delta":
		if block := s.thinking[index]; block != nil && response.Delta != nil {
			switch response.Delta.Type {
			case "thinking_delta":
				if response.Delta.Thinking != nil {
					value := ""
					if block.Thinking != nil {
						value = *block.Thinking
					}
					value += *response.Delta.Thinking
					block.Thinking = kitutil.GetPointer(value)
					events = append(events, s.Chat.AppendReasoning(*response.Delta.Thinking)...)
				}
			case "signature_delta":
				block.Signature += response.Delta.Signature
			}
			return events, nil
		}
	case "content_block_stop":
		if block := s.thinking[index]; block != nil {
			encrypted := ""
			if block.Signature != "" || block.Data != "" {
				var err error
				encrypted, err = sharedclaude.EncodeThinkingBlock(*block)
				if err != nil {
					return nil, err
				}
			}
			delete(s.thinking, index)
			return s.Chat.CompleteReasoning(encrypted), nil
		}
	}
	hosted, handled, err := s.hosted.Convert(response, s.Chat)
	if err != nil || handled {
		return append(events, hosted...), err
	}
	chat, err := s.chat.ConvertChunk(response)
	if err != nil {
		return nil, err
	}
	converted, err := oaichat.ChatCompletionsStreamChunkToResponsesEvents(chat, s.Chat)
	events = append(events, converted...)
	if response.Type == "content_block_stop" {
		events = append(events, s.Chat.CloseContent()...)
		events = append(events, s.Chat.CloseTools()...)
	}
	return events, err
}

// CloseThinking preserves signatures even if a gateway omits content_block_stop.
func (s *ClaudeToResponsesStreamState) CloseThinking() ([]oaichat.ChatToResponsesStreamEvent, error) {
	indexes := make([]int, 0, len(s.thinking))
	for index := range s.thinking {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	var events []oaichat.ChatToResponsesStreamEvent
	for _, index := range indexes {
		block := s.thinking[index]
		encrypted := ""
		if block.Signature != "" || block.Data != "" {
			var err error
			encrypted, err = sharedclaude.EncodeThinkingBlock(*block)
			if err != nil {
				return nil, err
			}
		}
		events = append(events, s.Chat.CompleteReasoning(encrypted)...)
		delete(s.thinking, index)
	}
	return events, nil
}
