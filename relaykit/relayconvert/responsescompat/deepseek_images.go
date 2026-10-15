// Adapted from StarryKira/sub2api's DeepSeek image compatibility bridge.
// Original code is licensed under LGPL-3.0; see LGPL-3.0.txt.
package responsescompat

import "strings"

// AliasDeepSeekResponsesInputImages 把图片 part 改写成 DeepSeek 能反序列化
// 的形状：type=input_image，同时带字符串字段 image_url 与 url。
// 文档与部分 400 提 image_url；线上 serde 要求 url。只发其中一个都会踩坑。
// 仅有 file_id、没有可用 URL 的 part 原样保留。
func AliasDeepSeekResponsesInputImages(input any) (any, bool) {
	items, ok := asDeepSeekResponsesSlice(input)
	if !ok {
		return input, false
	}
	changed := false
	for i, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if aliasDeepSeekResponsesInputItem(item) {
			items[i] = item
			changed = true
		}
	}
	if !changed {
		return input, false
	}
	return items, true
}

func aliasDeepSeekResponsesInputItem(item map[string]any) bool {
	changed := aliasDeepSeekResponsesImagePart(item)
	if content, exists := item["content"]; exists {
		if rewritten, did := aliasDeepSeekResponsesContent(content); did {
			item["content"] = rewritten
			changed = true
		}
	}
	if output, exists := item["output"]; exists {
		if rewritten, did := aliasDeepSeekResponsesContent(output); did {
			item["output"] = rewritten
			changed = true
		}
	}
	return changed
}

func aliasDeepSeekResponsesContent(content any) (any, bool) {
	parts, ok := asDeepSeekResponsesSlice(content)
	if !ok {
		return content, false
	}
	changed := false
	for i, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if aliasDeepSeekResponsesImagePart(part) {
			parts[i] = part
			changed = true
		}
	}
	if !changed {
		return content, false
	}
	return parts, true
}

func aliasDeepSeekResponsesImagePart(part map[string]any) bool {
	partType := strings.TrimSpace(stringValue(part["type"]))
	switch partType {
	case "input_image", "image_url", "image":
	default:
		return false
	}
	imageURL := extractDeepSeekResponsesImageURL(part)
	if imageURL == "" {
		return false
	}
	changed := false
	if partType != "input_image" {
		part["type"] = "input_image"
		changed = true
	}
	if current, ok := part["image_url"].(string); !ok || strings.TrimSpace(current) != imageURL {
		part["image_url"] = imageURL
		changed = true
	}
	if current, ok := part["url"].(string); !ok || strings.TrimSpace(current) != imageURL {
		part["url"] = imageURL
		changed = true
	}
	return changed
}

func extractDeepSeekResponsesImageURL(part map[string]any) string {
	if imageURL := deepSeekResponsesURLValue(part["url"]); imageURL != "" {
		return imageURL
	}
	if imageURL := deepSeekResponsesURLValue(part["image_url"]); imageURL != "" {
		return imageURL
	}
	if imageURL := deepSeekResponsesURLValue(part["image"]); imageURL != "" {
		return imageURL
	}
	source, ok := part["source"].(map[string]any)
	if !ok {
		return ""
	}
	if imageURL := deepSeekResponsesURLValue(source["url"]); imageURL != "" {
		return imageURL
	}
	data := strings.TrimSpace(stringValue(source["data"]))
	if data == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(data), "data:") {
		return data
	}
	mediaType := strings.TrimSpace(stringValue(source["media_type"]))
	if mediaType == "" {
		mediaType = "image/png"
	}
	return "data:" + mediaType + ";base64," + data
}

func deepSeekResponsesURLValue(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		if imageURL := strings.TrimSpace(stringValue(typed["url"])); imageURL != "" {
			return imageURL
		}
		return strings.TrimSpace(stringValue(typed["image_url"]))
	default:
		return ""
	}
}

func asDeepSeekResponsesSlice(value any) ([]any, bool) {
	switch items := value.(type) {
	case []any:
		return items, true
	case []map[string]any:
		out := make([]any, len(items))
		for i := range items {
			out[i] = items[i]
		}
		return out, true
	default:
		return nil, false
	}
}
