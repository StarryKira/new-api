package claudemessages

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// ClaudeMessagesRequestToGemini preserves the ordered content blocks and their
// individual signatures. Chat's separate content/reasoning/tool fields cannot
// represent that ordering, so only request configuration uses the Chat bridge.
func ClaudeMessagesRequestToGemini(ctx context.Context, request dto.ClaudeRequest, info convmeta.Meta) (*dto.GeminiChatRequest, error) {
	configRequest := request
	configRequest.Messages = nil
	chat, err := ClaudeMessagesRequestToOpenAIChat(ctx, configRequest, info)
	if err != nil {
		return nil, err
	}
	out, err := oaichat.OpenAIChatRequestToGeminiGenerateContent(ctx, *chat, info)
	if err != nil {
		return nil, err
	}
	out.Contents = nil
	out.SystemInstructions = nil
	if request.System != nil {
		system := request.ParseSystem()
		if request.IsStringSystem() {
			text := request.GetStringSystem()
			system = []dto.ClaudeMediaMessage{{Type: "text", Text: &text}}
		}
		parts, err := claudeBlocksToGemini(ctx, system, make(map[string]string))
		if err != nil {
			return nil, fmt.Errorf("system: %w", err)
		}
		if len(parts) > 0 {
			out.SystemInstructions = &dto.GeminiChatContent{Parts: parts}
		}
	}
	names := make(map[string]string)
	for index, message := range request.Messages {
		var blocks []dto.ClaudeMediaMessage
		if message.IsStringContent() {
			text := message.GetStringContent()
			blocks = []dto.ClaudeMediaMessage{{Type: "text", Text: &text}}
		} else {
			blocks, err = message.ParseContent()
			if err != nil {
				return nil, fmt.Errorf("messages[%d].content: %w", index, err)
			}
		}
		parts, err := claudeBlocksToGemini(ctx, blocks, names)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].content: %w", index, err)
		}
		if len(parts) == 0 {
			continue
		}
		if message.Role == "system" || message.Role == "developer" {
			if out.SystemInstructions == nil {
				out.SystemInstructions = &dto.GeminiChatContent{}
			}
			out.SystemInstructions.Parts = append(out.SystemInstructions.Parts, parts...)
			continue
		}
		role := message.Role
		if role == "assistant" {
			role = "model"
		}
		out.Contents = append(out.Contents, dto.GeminiChatContent{Role: role, Parts: parts})
	}
	return out, nil
}

func claudeBlocksToGemini(ctx context.Context, blocks []dto.ClaudeMediaMessage, names map[string]string) ([]dto.GeminiPart, error) {
	parts := make([]dto.GeminiPart, 0, len(blocks))
	for _, block := range blocks {
		part := dto.GeminiPart{}
		if block.Signature != "" {
			part.ThoughtSignature, _ = kitutil.Marshal(block.Signature)
		}
		switch block.Type {
		case "text":
			part.Text = block.GetText()
			if part.Text == "" && len(part.ThoughtSignature) == 0 {
				continue
			}
		case "thinking":
			part.Thought = true
			if block.Thinking != nil {
				part.Text = *block.Thinking
			}
		case "redacted_thinking":
			return nil, fmt.Errorf("Gemini cannot represent redacted_thinking")
		case "image", "document":
			if block.Source == nil {
				return nil, fmt.Errorf("%s source is required", block.Type)
			}
			if block.Source.Type == "text" {
				text, ok := block.Source.Data.(string)
				if !ok {
					return nil, fmt.Errorf("text document data must be a string")
				}
				part.Text = text
			} else {
				source := block.ToFileSource()
				if source == nil {
					return nil, fmt.Errorf("%s source data is required", block.Type)
				}
				data, mime, err := relaymedia.ResolveBase64Data(ctx, source, "formatting Claude media for Gemini")
				if err != nil {
					return nil, err
				}
				if !sharedgemini.IsSupportedMimeType(mime) {
					return nil, fmt.Errorf("Gemini does not support media type %q", mime)
				}
				part.InlineData = &dto.GeminiInlineData{MimeType: mime, Data: data}
			}
		case "tool_use":
			args := map[string]any{}
			if block.Input != nil {
				data, err := kitutil.Marshal(block.Input)
				if err != nil {
					return nil, err
				}
				if err := kitutil.Unmarshal(data, &args); err != nil {
					return nil, fmt.Errorf("tool %s input: %w", block.Name, err)
				}
			}
			part.FunctionCall = &dto.FunctionCall{ID: block.Id, FunctionName: block.Name, Arguments: args}
			names[block.Id] = block.Name
		case "tool_result":
			name := block.Name
			if name == "" {
				name = names[block.ToolUseId]
			}
			if name == "" {
				name = block.ToolUseId
			}
			text, media, err := claudeToolResultForGemini(ctx, block, names)
			if err != nil {
				return nil, err
			}
			key := "result"
			if block.IsError != nil && *block.IsError {
				key = "error"
			}
			id, _ := kitutil.Marshal(block.ToolUseId)
			part.FunctionResponse = &dto.GeminiFunctionResponse{Name: name, ID: id, Response: map[string]any{key: text}}
			parts = append(parts, part)
			parts = append(parts, media...)
			continue
		default:
			return nil, fmt.Errorf("unsupported Claude content block %q", block.Type)
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func claudeToolResultForGemini(ctx context.Context, block dto.ClaudeMediaMessage, names map[string]string) (any, []dto.GeminiPart, error) {
	if text, ok := block.Content.(string); ok {
		return text, nil, nil
	}
	if block.Content == nil {
		return "", nil, nil
	}
	data, err := kitutil.Marshal(block.Content)
	if err != nil {
		return nil, nil, err
	}
	var nested []dto.ClaudeMediaMessage
	if kitutil.Unmarshal(data, &nested) != nil {
		return block.Content, nil, nil
	}
	parts, err := claudeBlocksToGemini(ctx, nested, names)
	if err != nil {
		return nil, nil, err
	}
	texts := make([]string, 0, len(parts))
	var media []dto.GeminiPart
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
		if part.InlineData != nil {
			media = append(media, part)
		}
	}
	return strings.Join(texts, "\n"), media, nil
}
