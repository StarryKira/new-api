// Package responsescompat adapts native Responses dialect features for
// function-only upstreams without converting the enclosing protocol.
package responsescompat

import "encoding/json"

// These helpers expose the same ID and deferred-tool rules to compatibility
// facades; callers do not need to implement wire transformations themselves.
func RetypedResponsesToolCallItemID(id, itemType string) string {
	return retypedResponsesToolCallItemID(id, itemType)
}
func NormalizeLoweredFunctionItemID(item map[string]any) { normalizeLoweredFunctionItemID(item) }
func StripResponsesDeferredToolFlags(tools []any) bool   { return stripResponsesDeferredToolFlags(tools) }
func PromotedResponsesToolSearchDiscoveries(tools, input []any) ([]any, error) {
	return promotedResponsesToolSearchDiscoveries(tools, input)
}
func CopyClientTool(tool map[string]any) map[string]any { return copyClientTool(tool) }

func FlattenNamespaceToolName(namespace, name string) string {
	return flattenNamespaceToolName(namespace, name)
}
func ToolSearchCallArgumentsJSON(arguments string) json.RawMessage {
	return toolSearchCallArgumentsJSON(arguments)
}
func ExtractCustomToolCallInput(arguments string) string {
	return extractCustomToolCallInput(arguments)
}

// ExtractToolOutputImages removes recognized images from nested JSON tool output
// while retaining all text, object fields, and numeric lexemes.
func ExtractToolOutputImages(raw json.RawMessage) (string, []string, bool) {
	text, parts, changed := extractToolOutputMedia(raw)
	urls := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.ImageURL != nil {
			urls = append(urls, part.ImageURL.URL)
		}
	}
	return text, urls, changed
}
