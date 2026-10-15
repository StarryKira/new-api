package oaichat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestGeminiRequestPreservesRealToolAndThinkingSignatures(t *testing.T) {
	thinking := "plan"
	message := dto.Message{Role: "assistant", Content: "calling tool", ReasoningContent: &thinking, ReasoningSignature: "thought-sig"}
	message.SetToolCalls([]dto.ToolCallRequest{{ID: "call_1", Type: "function", Function: dto.FunctionRequest{Name: "lookup", Arguments: `{"q":"x"}`}, ExtraContent: dto.GoogleThoughtSignature(json.RawMessage(`"real-sig"`))}})
	req, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), dto.GeneralOpenAIRequest{Model: "gemini-2.5-flash", Messages: []dto.Message{message}}, &convmeta.Values{})
	require.NoError(t, err)
	require.Len(t, req.Contents, 1)
	require.Equal(t, "plan", req.Contents[0].Parts[0].Text)
	require.JSONEq(t, `"thought-sig"`, string(req.Contents[0].Parts[0].ThoughtSignature))
	var tool *dto.GeminiPart
	for i := range req.Contents[0].Parts {
		if req.Contents[0].Parts[i].FunctionCall != nil {
			tool = &req.Contents[0].Parts[i]
		}
	}
	require.NotNil(t, tool)
	require.Equal(t, "call_1", tool.FunctionCall.ID)
	require.JSONEq(t, `"real-sig"`, string(tool.ThoughtSignature))
}

func TestGeminiToolResultsAndPDFKeepMedia(t *testing.T) {
	relaymedia.SetMediaResolver(relaymedia.MediaResolver{GetBase64Data: func(_ context.Context, source types.FileSource, _ ...string) (string, string, error) {
		raw := source.GetRawData()
		head, data, _ := strings.Cut(raw, ",")
		mime := strings.TrimSuffix(strings.TrimPrefix(head, "data:"), ";base64")
		return data, mime, nil
	}})
	t.Cleanup(func() { relaymedia.SetMediaResolver(relaymedia.MediaResolver{}) })
	tool := dto.Message{Role: "tool", ToolCallId: "call_1"}
	tool.SetMediaContent([]dto.MediaContent{{Type: "text", Text: "screenshot"}, {Type: "image_url", ImageUrl: &dto.MessageImageUrl{Url: "data:image/png;base64,aW1hZ2U="}}})
	user := dto.Message{Role: "user"}
	user.SetMediaContent([]dto.MediaContent{{Type: "file", File: &dto.MessageFile{FileName: "document.pdf", FileData: "data:application/pdf;base64,cGRm"}}})
	req, err := OpenAIChatRequestToGeminiGenerateContent(context.Background(), dto.GeneralOpenAIRequest{Model: "gemini-2.5-flash", Messages: []dto.Message{tool, user}}, &convmeta.Values{})
	require.NoError(t, err)
	require.Len(t, req.Contents, 1)
	require.Len(t, req.Contents[0].Parts, 3)
	require.Equal(t, "screenshot", req.Contents[0].Parts[0].FunctionResponse.Response["content"])
	require.Equal(t, "image/png", req.Contents[0].Parts[1].InlineData.MimeType)
	require.Equal(t, "aW1hZ2U=", req.Contents[0].Parts[1].InlineData.Data)
	require.Equal(t, "application/pdf", req.Contents[0].Parts[2].InlineData.MimeType)
	require.Equal(t, "cGRm", req.Contents[0].Parts[2].InlineData.Data)
}

func TestGeminiResponseAndStreamPreserveReasoningSignatures(t *testing.T) {
	reasoning := "thought"
	got := ResponseOpenAI2Gemini(&dto.OpenAITextResponse{Choices: []dto.OpenAITextResponseChoice{{Message: dto.Message{Role: "assistant", Content: "answer", ReasoningContent: &reasoning, ReasoningSignature: "sig"}}}}, nil)
	require.Len(t, got.Candidates[0].Content.Parts, 2)
	require.True(t, got.Candidates[0].Content.Parts[0].Thought)
	require.JSONEq(t, `"sig"`, string(got.Candidates[0].Content.Parts[0].ThoughtSignature))
	state := NewChatToGeminiStreamState()
	chunks, err := state.ConvertChunk(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningSignature: "signature-only"}}}}, nil)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	require.Len(t, chunks[0].Candidates[0].Content.Parts, 1)
	require.JSONEq(t, `"signature-only"`, string(chunks[0].Candidates[0].Content.Parts[0].ThoughtSignature))
}

func TestGeminiStreamRetainsToolSignatureAcrossArguments(t *testing.T) {
	state := NewChatToGeminiStreamState()
	index := 0
	_, err := state.ConvertChunk(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{Index: &index, ID: "call_1", Type: "function", Function: dto.FunctionResponse{Name: "lookup", Arguments: `{"q":`}, ExtraContent: dto.GoogleThoughtSignature(json.RawMessage(`"sig"`))}}}}}}, nil)
	require.NoError(t, err)
	finish := "tool_calls"
	chunks, err := state.ConvertChunk(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &finish, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{Index: &index, Function: dto.FunctionResponse{Arguments: `"x"}`}}}}}}}, nil)
	require.NoError(t, err)
	require.Len(t, chunks, 1)
	parts := chunks[0].Candidates[0].Content.Parts
	require.Len(t, parts, 1)
	require.Equal(t, "call_1", parts[0].FunctionCall.ID)
	require.Equal(t, map[string]any{"q": "x"}, parts[0].FunctionCall.Arguments)
	require.JSONEq(t, `"sig"`, string(parts[0].ThoughtSignature))
}
