package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// sub2api 8c1233393: replacing a real Gemini thought signature with a dummy
// breaks multi-turn tool calls. The Chat pivot must retain both signatures.
func TestGeminiClaudeRequestSignatureRoundTrip(t *testing.T) {
	var original dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gemini-2.5-flash","max_tokens":2048,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"planning","signature":"thought-signature"},{"type":"tool_use","id":"call_1","name":"lookup","input":{"q":"x"},"signature":"tool-signature"}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"ok"}]}]}`), &original))
	meta := &convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "gemini-2.5-flash"}
	result, err := ConvertRequest(context.Background(), meta, types.RelayFormatGemini, &original)
	require.NoError(t, err)
	gemini := result.Value.(*dto.GeminiChatRequest)
	require.Len(t, gemini.Contents, 2)
	require.JSONEq(t, `"thought-signature"`, string(gemini.Contents[0].Parts[0].ThoughtSignature))
	require.JSONEq(t, `"tool-signature"`, string(gemini.Contents[0].Parts[1].ThoughtSignature))
	result, err = ConvertRequest(context.Background(), &convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "claude-sonnet-4-5"}, types.RelayFormatClaude, gemini)
	require.NoError(t, err)
	claude := result.Value.(*dto.ClaudeRequest)
	blocks, err := claude.Messages[1].ParseContent()
	require.NoError(t, err)
	var thought, tool *dto.ClaudeMediaMessage
	for i := range blocks {
		if blocks[i].Type == "thinking" {
			thought = &blocks[i]
		}
		if blocks[i].Type == "tool_use" {
			tool = &blocks[i]
		}
	}
	require.NotNil(t, thought)
	require.NotNil(t, tool)
	require.Equal(t, "thought-signature", thought.Signature)
	require.Equal(t, "tool-signature", tool.Signature)
	require.Equal(t, "call_1", tool.Id)
}

// sub2api 0a521f09f: a Gemini tool→text transition must close the Anthropic
// tool block before opening text, including when both arrive in one chunk.
func TestGeminiClaudeStreamKeepsContentBlockOrder(t *testing.T) {
	var chunk dto.GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"thought":true,"text":"plan","thoughtSignature":"thought-sig"},{"functionCall":{"id":"call_1","name":"lookup","args":{"q":"x"}},"thoughtSignature":"tool-sig"},{"text":"done"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":70,"candidatesTokenCount":9,"thoughtsTokenCount":4,"totalTokenCount":113}}`), &chunk))
	state, err := NewResponseStreamState(types.RelayFormatGemini, types.RelayFormatClaude, ResponseStreamOptions{ID: "fixed", Created: 1})
	require.NoError(t, err)
	meta := &convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "gemini-2.5-flash"}
	results, err := ConvertStreamResponseChunk(context.Background(), meta, state, &chunk)
	require.NoError(t, err)
	finals, err := FinalizeStreamResponse(context.Background(), meta, state)
	require.NoError(t, err)
	results = append(results, finals...)
	open := -1
	var starts []string
	stops := 0
	messageStarts := 0
	for _, result := range results {
		encoded, err := kitutil.Marshal(result.Value)
		require.NoError(t, err)
		var event map[string]any
		require.NoError(t, kitutil.Unmarshal(encoded, &event))
		switch event["type"] {
		case "message_start":
			messageStarts++
		case "content_block_start":
			require.Equal(t, -1, open, "overlapping block: %s", encoded)
			open = int(event["index"].(float64))
			block := event["content_block"].(map[string]any)
			starts = append(starts, block["type"].(string))
		case "content_block_stop":
			require.Equal(t, open, int(event["index"].(float64)), "mismatched close: %s", encoded)
			open = -1
		case "message_stop":
			stops++
		}
	}
	require.Equal(t, []string{"thinking", "tool_use", "text"}, starts)
	require.Equal(t, -1, open)
	require.Equal(t, 1, stops)
	require.Equal(t, 1, messageStarts)
}

func TestGeminiClaudeFullResponseKeepsPartOrderAndSignatures(t *testing.T) {
	var response dto.GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"thought":true,"text":"first thought","thoughtSignature":"first-sig"},{"text":"before"},{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}},{"functionCall":{"id":"call_1","name":"lookup","args":{"q":"x"}},"thoughtSignature":"tool-sig"},{"thought":true,"text":"second thought","thoughtSignature":"second-sig"},{"text":"after"}]},"finishReason":"STOP","groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://example.com/result","title":"Result"}}],"groundingSupports":[{"segment":{"partIndex":5,"startIndex":0,"endIndex":5,"text":"after"},"groundingChunkIndices":[0]}]}}],"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":70,"candidatesTokenCount":9,"thoughtsTokenCount":4,"totalTokenCount":113}}`), &response))
	meta := &convmeta.Values{ChannelMetaAttached: true, UpstreamModelName: "gemini-test"}
	result, err := ConvertResponse(context.Background(), meta, types.RelayFormatClaude, &response)
	require.NoError(t, err)
	claude := result.Value.(*dto.ClaudeResponse)
	require.Len(t, claude.Content, 6)
	require.Equal(t, "first-sig", claude.Content[0].Signature)
	require.Equal(t, "before", claude.Content[1].GetText())
	require.Equal(t, "![image](data:image/png;base64,aW1hZ2U=)", claude.Content[2].GetText())
	require.Equal(t, "tool_use", claude.Content[3].Type)
	require.Equal(t, "call_1", claude.Content[3].Id)
	require.Equal(t, "tool-sig", claude.Content[3].Signature)
	require.Equal(t, "second-sig", claude.Content[4].Signature)
	require.Equal(t, "after", claude.Content[5].GetText())
	require.Empty(t, claude.Content[1].Citations)
	require.Contains(t, string(claude.Content[5].Citations), "https://example.com/result")
	require.Contains(t, string(claude.Content[5].Citations), `"cited_text":"after"`)
	require.Equal(t, "tool_use", claude.StopReason)
	require.Equal(t, "gemini-test", claude.Model)
	require.Equal(t, 100, result.Usage.PromptTokens)
	require.Equal(t, 70, claude.Usage.CacheReadInputTokens)
	require.Equal(t, 13, claude.Usage.OutputTokens)
}
