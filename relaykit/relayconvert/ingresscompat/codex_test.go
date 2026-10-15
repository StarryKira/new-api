// Adapted from StarryKira/sub2api regression tests; LGPL-3.0.
// See ../responsescompat/LGPL-3.0.txt.
package ingresscompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyCodexOAuthTransform_ConvertsToolRoleMessageToFunctionCallOutput(t *testing.T) {
	reqBody := map[string]any{
		"model": "gpt-5.4",
		"input": []any{
			map[string]any{
				"type":         "message",
				"role":         "tool",
				"tool_call_id": "call_1",
				"content":      "ok",
			},
		},
	}

	normalizeTestCodexIngress(reqBody)

	input, ok := reqBody["input"].([]any)
	require.True(t, ok)
	require.Len(t, input, 1)

	item, ok := input[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "function_call_output", item["type"])
	require.Equal(t, "call_1", item["call_id"])
	require.Equal(t, "ok", item["output"])
	_, hasRole := item["role"]
	require.False(t, hasRole)
}

func TestApplyCodexOAuthTransform_StringifiesNonStringMessageContentText(t *testing.T) {
	reqBody := map[string]any{
		"model": "gpt-5.4",
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": []any{"a", "b"}},
				},
			},
		},
	}

	normalizeTestCodexIngress(reqBody)

	input, ok := reqBody["input"].([]any)
	require.True(t, ok)
	item, ok := input[0].(map[string]any)
	require.True(t, ok)
	content, ok := item["content"].([]any)
	require.True(t, ok)
	part, ok := content[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, `["a","b"]`, part["text"])
}

func TestNormalizeCodexToolChoice_InvalidAllowedToolsNeverBecomesAuto(t *testing.T) {
	for _, choice := range []map[string]any{
		{"type": "allowed_tools"},
		{"type": "allowed_tools", "mode": "invalid", "tools": []any{}},
		{"type": "allowed_tools", "mode": "required", "tools": "invalid"},
		{"type": "allowed_tools", "mode": "required", "tools": []any{map[string]any{"type": "function", "name": "missing"}}},
	} {
		reqBody := map[string]any{"tool_choice": choice}
		require.False(t, NormalizeCodexToolChoice(reqBody))
		// The upstream owns schema validation. A malformed restriction must never
		// silently become permission to call every supplied tool.
		require.Equal(t, choice, reqBody["tool_choice"])
	}
}

func TestApplyCodexOAuthTransform_DowngradesUnknownToolChoice(t *testing.T) {
	reqBody := map[string]any{
		"model": "gpt-5.4",
		"tools": []any{
			map[string]any{"type": "function", "name": "shell"},
		},
		"tool_choice": map[string]any{"type": "custom"},
	}

	normalizeTestCodexIngress(reqBody)

	require.Equal(t, "auto", reqBody["tool_choice"])
}

func TestApplyCodexOAuthTransform_PreservesKnownToolChoice(t *testing.T) {
	reqBody := map[string]any{
		"model": "gpt-5.4",
		"tools": []any{
			map[string]any{"type": "custom", "name": "shell"},
		},
		"tool_choice": map[string]any{"type": "custom"},
	}

	normalizeTestCodexIngress(reqBody)

	choice, ok := reqBody["tool_choice"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "custom", choice["type"])
}

func TestApplyCodexOAuthTransform_NormalizesLegacyFunctionToolChoice(t *testing.T) {
	reqBody := map[string]any{
		"model": "gpt-5.4",
		"tools": []any{
			map[string]any{"type": "function", "name": "shell"},
		},
		"tool_choice": map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "shell"},
		},
	}

	normalizeTestCodexIngress(reqBody)

	choice, ok := reqBody["tool_choice"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "function", choice["type"])
	require.Equal(t, "shell", choice["name"])
	require.NotContains(t, choice, "function")
}

func TestApplyCodexOAuthTransform_DowngradesMissingFunctionToolChoice(t *testing.T) {
	reqBody := map[string]any{
		"model": "gpt-5.4",
		"tools": []any{
			map[string]any{"type": "function", "name": "shell"},
		},
		"tool_choice": map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "missing"},
		},
	}

	normalizeTestCodexIngress(reqBody)

	require.Equal(t, "auto", reqBody["tool_choice"])
}

func TestApplyCodexOAuthTransform_NormalizeCodexTools_PreservesResponsesFunctionTools(t *testing.T) {
	reqBody := map[string]any{
		"model": "gpt-5.1",
		"tools": []any{
			map[string]any{
				"type":        "function",
				"name":        "bash",
				"description": "desc",
				"parameters":  map[string]any{"type": "object"},
			},
			map[string]any{
				"type":     "function",
				"function": nil,
			},
		},
	}

	normalizeTestCodexIngress(reqBody)

	tools, ok := reqBody["tools"].([]any)
	require.True(t, ok)
	require.Len(t, tools, 1)

	first, ok := tools[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "function", first["type"])
	require.Equal(t, "bash", first["name"])
}
func normalizeTestCodexIngress(body map[string]any) {
	NormalizeCodexLegacyTools(body)
	NormalizeCodexTools(body)
	NormalizeCodexToolChoice(body)
	if input, ok := body["input"].([]any); ok {
		input, _ = NormalizeCodexToolRoleMessages(input)
		input, _ = NormalizeCodexMessageContentText(input)
		body["input"] = input
	}
}

func TestCodexLegacyToolsPreserveSchemasAndExtensions(t *testing.T) {
	body := map[string]any{
		"functions":     []any{map[string]any{"name": "lookup", "strict": false, "parameters": map[string]any{"type": "object", "additionalProperties": false}}},
		"function_call": map[string]any{"name": "lookup"},
		"metadata":      map[string]any{"large": json.Number("9007199254740993")},
	}
	normalizeTestCodexIngress(body)
	require.NotContains(t, body, "functions")
	require.NotContains(t, body, "function_call")
	choice := body["tool_choice"].(map[string]any)
	require.Equal(t, "lookup", choice["name"])
	tool := body["tools"].([]any)[0].(map[string]any)
	require.Equal(t, false, tool["strict"])
	require.Equal(t, false, tool["parameters"].(map[string]any)["additionalProperties"])
	require.Equal(t, json.Number("9007199254740993"), body["metadata"].(map[string]any)["large"])
}
