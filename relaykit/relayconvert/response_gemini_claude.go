package relayconvert

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	geminichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/gemini_chat"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func convertGeminiChatResponseToClaudeMessages(_ context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error) {
	gemini, err := asGeminiChatResponse(response)
	if err != nil {
		return nil, nil, err
	}
	usage := UsageFromGeminiMetadata(gemini.GetUsageMetadata(), fallbackPromptTokens(info))
	result := geminichat.ResponseGeminiChat2Claude(fmt.Sprintf("chatcmpl-%s", kitutil.GetUUID()), kitutil.GetTimestamp(), gemini, info)
	if info != nil && info.HasChannelMeta() {
		result.Model = info.GetUpstreamModelName()
	}
	result.Usage = sharedclaude.UsageFromOpenAI(usage)
	return result, usage, nil
}

func convertGeminiChatStreamResponseToClaudeMessages(ctx context.Context, info convmeta.Meta, response any) (any, *dto.Usage, error) {
	chat, usage, err := convertGeminiChatStreamResponseToOAIChat(ctx, info, response)
	if err != nil || chat == nil {
		return nil, usage, err
	}
	claude, _, err := convertOAIChatStreamResponseToClaudeMessages(ctx, info, chat)
	return claude, usage, err
}

func convertGeminiChatStreamResponseChunkToClaudeMessages(ctx context.Context, info convmeta.Meta, response any, state any) ([]any, *dto.Usage, error) {
	chunks, usage, err := convertGeminiChatStreamResponseChunkToOAIChat(ctx, info, response, state)
	if err != nil {
		return nil, nil, err
	}
	return geminiChatChunksToClaude(ctx, info, chunks, usage)
}

func finalizeGeminiChatStreamResponseToClaudeMessages(ctx context.Context, info convmeta.Meta, state any) ([]any, *dto.Usage, error) {
	chunks, usage, err := finalizeGeminiChatStreamResponseToOAIChat(ctx, info, state)
	if err != nil {
		return nil, nil, err
	}
	converted, usage, err := geminiChatChunksToClaude(ctx, info, chunks, usage)
	if err != nil {
		return nil, nil, err
	}
	final, finalUsage, err := finalizeOAIChatStreamResponseToClaudeMessages(ctx, info, nil)
	if err != nil {
		return nil, nil, err
	}
	if finalUsage != nil {
		usage = finalUsage
	}
	return append(converted, final...), usage, nil
}

func geminiChatChunksToClaude(ctx context.Context, info convmeta.Meta, chunks []any, usage *dto.Usage) ([]any, *dto.Usage, error) {
	var results []any
	for _, chunk := range chunks {
		// This internal Chat stage bypasses registry preparation, so advance
		// the same counter the registered Chat-to-Claude route uses.
		if info != nil {
			info.IncrSendResponseCount()
		}
		converted, chunkUsage, err := convertOAIChatStreamResponseToClaudeMessages(ctx, info, chunk)
		if err != nil {
			return nil, nil, err
		}
		results = append(results, streamValuesFromAny(converted)...)
		if chunkUsage != nil {
			usage = chunkUsage
		}
	}
	return results, usage, nil
}
