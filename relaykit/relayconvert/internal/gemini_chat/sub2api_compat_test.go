package geminichat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
)

func TestGeminiRequestRetainsTextToolsAndRealSignatures(t *testing.T) {
	req := &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{{Role: "model", Parts: []dto.GeminiPart{
		{Thought: true, Text: "planning", ThoughtSignature: json.RawMessage(`"reasoning-signature"`)},
		{Text: "Calling the weather tool"},
		{FunctionCall: &dto.FunctionCall{ID: "call_7", FunctionName: "weather", Arguments: map[string]any{"city": "Tokyo"}}, ThoughtSignature: json.RawMessage(`"real-tool-signature"`)},
	}}}}
	got, err := GeminiGenerateContentRequestToOpenAIChat(context.Background(), req, &convmeta.Values{})
	require.NoError(t, err)
	require.Len(t, got.Messages, 1)
	require.Equal(t, "Calling the weather tool", got.Messages[0].StringContent())
	require.Equal(t, "planning", got.Messages[0].GetReasoningContent())
	require.Equal(t, "reasoning-signature", got.Messages[0].ReasoningSignature)
	calls := got.Messages[0].ParseToolCalls()
	require.Len(t, calls, 1)
	require.Equal(t, "call_7", calls[0].ID)
	require.JSONEq(t, `"real-tool-signature"`, string(calls[0].ExtraContent.ThoughtSignature()))
}

func TestGeminiStreamPreservesMixedPartOrderAndSignatures(t *testing.T) {
	finish := "STOP"
	response := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{
		{Thought: true, Text: "plan", ThoughtSignature: json.RawMessage(`"reasoning-signature"`)},
		{FunctionCall: &dto.FunctionCall{ID: "call_1", FunctionName: "lookup", Arguments: map[string]any{"q": "x"}}, ThoughtSignature: json.RawMessage(`"tool-signature"`)},
		{Text: "done"},
	}}, FinishReason: &finish}}}
	state := NewGeminiToChatStreamState("chat_fixed", 1)
	usage := &dto.Usage{PromptTokens: 11, CompletionTokens: 4, TotalTokens: 15}
	chunks, err := state.ConvertChunk(response, "gemini", usage)
	require.NoError(t, err)
	require.Len(t, chunks, 4)
	require.Equal(t, "plan", chunks[0].Choices[0].Delta.GetReasoningContent())
	require.Empty(t, chunks[0].Choices[0].Delta.GetContentString())
	require.Equal(t, "reasoning-signature", chunks[0].Choices[0].Delta.ReasoningSignature)
	require.Len(t, chunks[1].Choices[0].Delta.ToolCalls, 1)
	require.Equal(t, 0, *chunks[1].Choices[0].Delta.ToolCalls[0].Index)
	require.JSONEq(t, `"tool-signature"`, string(chunks[1].Choices[0].Delta.ToolCalls[0].ExtraContent.ThoughtSignature()))
	require.Equal(t, "done", chunks[2].Choices[0].Delta.GetContentString())
	require.Empty(t, chunks[2].Choices[0].Delta.GetReasoningContent())
	require.Equal(t, "tool_calls", *chunks[3].Choices[0].FinishReason)
	require.Equal(t, usage, chunks[3].Usage)
	final, err := state.Finalize("gemini")
	require.NoError(t, err)
	require.Empty(t, final)
}

func TestGeminiPartialFunctionCallRetainsInitialSignature(t *testing.T) {
	state := NewGeminiToChatStreamState("chat_fixed", 1)
	continuing := true
	first := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{
		FunctionCall: &dto.FunctionCall{ID: "call_1", FunctionName: "lookup", Arguments: map[string]any{"q": "x"}, WillContinue: &continuing}, ThoughtSignature: json.RawMessage(`"signed-first-part"`),
	}}}}}}
	_, err := state.ConvertChunk(first, "gemini", nil)
	require.NoError(t, err)
	continuing = false
	last := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{WillContinue: &continuing}}}}}}}
	chunks, err := state.ConvertChunk(last, "gemini", nil)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Len(t, chunks[0].Choices[0].Delta.ToolCalls, 1)
	require.JSONEq(t, `"signed-first-part"`, string(chunks[0].Choices[0].Delta.ToolCalls[0].ExtraContent.ThoughtSignature()))
}

func TestGeminiNonStreamingConcatenatesThinkingAndKeepsChoiceFinishIndependent(t *testing.T) {
	response := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{
		{Index: 0, Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{FunctionName: "one", Arguments: map[string]any{}}}}}},
		{Index: 1, Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{Thought: true, Text: "first "}, {Thought: true, Text: "second", ThoughtSignature: json.RawMessage(`"sig"`)}, {Text: "answer"}}}},
	}}
	got := ResponseGeminiChat2OpenAI("id", 1, response)
	require.Equal(t, "tool_calls", got.Choices[0].FinishReason)
	require.Equal(t, "stop", got.Choices[1].FinishReason)
	require.Equal(t, "first second", got.Choices[1].Message.GetReasoningContent())
	require.Equal(t, "sig", got.Choices[1].Message.ReasoningSignature)
}

func TestGeminiUsagePreservesCacheAndReasoningWithoutCacheCreation(t *testing.T) {
	usage := UsageFromGeminiMetadata(&dto.GeminiUsageMetadata{PromptTokenCount: 100, CachedContentTokenCount: 70, CandidatesTokenCount: 9, ThoughtsTokenCount: 4, TotalTokenCount: 113}, 0)
	require.Equal(t, 100, usage.PromptTokens)
	require.Equal(t, 70, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 13, usage.CompletionTokens)
	require.Equal(t, 4, usage.CompletionTokenDetails.ReasoningTokens)
}

func TestGeminiCumulativeTextIsOptIn(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		state := NewGeminiToChatStreamState("id", 1)
		state.SetCumulativeText(enabled)
		for i, text := range []string{"hel", "hello"} {
			result, err := state.ConvertChunk(&dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Parts: []dto.GeminiPart{{Text: text}}}}}}, "model", nil)
			require.NoError(t, err)
			want := text
			if enabled && i == 1 {
				want = "lo"
			}
			require.Equal(t, want, result[0].Choices[0].Delta.GetContentString())
			if i == 0 {
				require.Equal(t, "assistant", result[0].Choices[0].Delta.Role)
			}
		}
	}
}

func TestGeminiOrdinaryTextSignaturePreserved(t *testing.T) {
	var response dto.GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"text":"answer","thoughtSignature":"sig-answer"}]},"finishReason":"STOP"}]}`), &response))
	state := NewGeminiToChatStreamState("test", 1)
	chunks, err := state.ConvertChunk(&response, "gemini", nil)
	require.NoError(t, err)
	var text, signature string
	for _, chunk := range chunks {
		for _, choice := range chunk.Choices {
			text += choice.Delta.GetContentString()
			signature += choice.Delta.ReasoningSignature
		}
	}
	require.Equal(t, "answer", text)
	require.Equal(t, "sig-answer", signature)
}
