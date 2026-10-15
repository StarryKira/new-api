// Portions derived from github.com/Wei-Shaw/sub2api, licensed under LGPLv3.
// See LGPL-3.0.txt in this package for the upstream license.
package responsescompat

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/tooloutput"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

// Native Responses dialect adaptations preserve unknown request and event fields.
const customToolInputSchema = `{"type":"object","properties":{"input":{"type":"string","description":"The raw input for this tool, passed through verbatim."}},"required":["input"]}`
const toolSearchProxyName = "tool_search"
const toolSearchProxySchema = `{"type":"object","properties":{"query":{"type":"string","description":"Search query for tools or connectors to load."},"limit":{"type":"integer","description":"Maximum number of tool groups to return."}},"required":["query"]}`
const toolOutputMediaMarker = "[Tool output media moved to the following user message]"
const toolOutputMediaAttribution = "[Tool output media for call %s]"
const chatToolNameMaxLen = 64

func bytesTrimSpace(raw []byte) []byte { return bytes.TrimSpace(raw) }
func extractToolOutputMedia(raw json.RawMessage) (string, []tooloutput.Part, bool) {
	return tooloutput.Extract(raw)
}

func extractCustomToolCallInput(arguments string) string {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := kitutil.Unmarshal([]byte(trimmed), &obj); err != nil {
		return trimmed
	}
	if raw, ok := obj["input"]; ok {
		var s string
		if err := kitutil.Unmarshal(raw, &s); err == nil {
			return s
		}
		return trimmed
	}
	if len(obj) == 0 {
		return ""
	}
	return trimmed
}

func toolSearchCallArgumentsJSON(arguments string) json.RawMessage {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	if kitutil.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	fallback, _ := kitutil.Marshal(arguments)
	return fallback
}

func flattenNamespaceToolName(namespace, name string) string {
	full := namespace + "__" + name
	if len(full) <= chatToolNameMaxLen {
		return full
	}
	sum := sha256.Sum256([]byte(full))
	suffix := "__" + hex.EncodeToString(sum[:4])
	prefixLen := chatToolNameMaxLen - len(suffix)
	var prefix strings.Builder
	for _, ch := range full {
		if prefix.Len()+len(string(ch)) > prefixLen {
			break
		}
		_, _ = prefix.WriteRune(ch)
	}
	return prefix.String() + suffix
}

// responsesToolChoiceToChatToolChoice 把 Responses 的 tool_choice 转为 chat 形态。
// declared 是转换后实际声明的 chat 工具名集合：具名选择项仅在目标工具幸存时转发，
// 服务端工具（web_search 等）的选择项随工具本身丢弃——指向未声明工具的 tool_choice
// 会被 chat 上游 400 拒绝。返回 nil 表示丢弃 tool_choice。
