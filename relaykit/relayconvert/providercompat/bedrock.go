// Adapted from StarryKira/sub2api's Bedrock compatibility bridge.
// Original code is licensed under LGPL-3.0; see ../responsescompat/LGPL-3.0.txt.
// Package providercompat owns provider-specific dialect adaptations. Transport,
// authentication and account policy remain in the embedding gateway.
package providercompat

import (
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// InlineBedrockOutputSchema adapts unsupported structured output to a prompt.
func InlineBedrockOutputSchema(body []byte) []byte {
	outputFormat := gjson.GetBytes(body, "output_config.format")
	if !outputFormat.Exists() {
		outputFormat = gjson.GetBytes(body, "output_format")
	}
	if !outputFormat.Exists() || !outputFormat.IsObject() {
		return body
	}

	// 先从请求体中移除两个版本的结构化输出字段。
	body, _ = sjson.DeleteBytes(body, "output_format")
	body, _ = sjson.DeleteBytes(body, "output_config.format")

	schema := outputFormat.Get("schema")
	if !schema.Exists() {
		return body
	}

	// 找到最后一条 user message
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}
	msgArr := messages.Array()
	lastUserIdx := -1
	for i := len(msgArr) - 1; i >= 0; i-- {
		if msgArr[i].Get("role").String() == "user" {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx < 0 {
		return body
	}

	// 将 schema 序列化为 JSON 文本追加到该 message 的 content 数组
	schemaJSON, err := kitutil.Marshal(json.RawMessage(schema.Raw))
	if err != nil {
		return body
	}

	content := msgArr[lastUserIdx].Get("content")
	basePath := fmt.Sprintf("messages.%d.content", lastUserIdx)

	if content.IsArray() {
		// 追加一个 text block 到 content 数组末尾
		idx := len(content.Array())
		body, _ = sjson.SetBytes(body, fmt.Sprintf("%s.%d.type", basePath, idx), "text")
		body, _ = sjson.SetBytes(body, fmt.Sprintf("%s.%d.text", basePath, idx), string(schemaJSON))
	} else if content.Type == gjson.String {
		// content 是纯字符串，转换为数组格式
		originalText := content.String()
		body, _ = sjson.SetBytes(body, basePath, []map[string]string{
			{"type": "text", "text": originalText},
			{"type": "text", "text": string(schemaJSON)},
		})
	}

	return body
}

// NormalizeBedrockInvocationMetrics adapts provider metrics without replacing native usage.
func NormalizeBedrockInvocationMetrics(data []byte) []byte {
	metrics := gjson.GetBytes(data, "amazon-bedrock-invocationMetrics")
	if !metrics.Exists() || !metrics.IsObject() {
		return data
	}

	// 移除 Bedrock 特有字段
	data, _ = sjson.DeleteBytes(data, "amazon-bedrock-invocationMetrics")

	// 如果已有标准 usage 字段，不覆盖
	if gjson.GetBytes(data, "usage").Exists() {
		return data
	}

	// 转换 camelCase → snake_case 写入 usage
	inputTokens := metrics.Get("inputTokenCount")
	outputTokens := metrics.Get("outputTokenCount")
	if inputTokens.Exists() {
		data, _ = sjson.SetBytes(data, "usage.input_tokens", inputTokens.Int())
	}
	if outputTokens.Exists() {
		data, _ = sjson.SetBytes(data, "usage.output_tokens", outputTokens.Int())
	}

	return data
}
