// Derived from github.com/Wei-Shaw/sub2api, licensed under LGPLv3.
// See ../../../responsescompat/LGPL-3.0.txt for the upstream license.
package tooloutput

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

const toolOutputMediaMarker = "[Tool output media moved to the following user message]"

type Part struct {
	Type     string
	ImageURL *ImageReference
}
type ImageReference struct{ URL string }

func bytesTrimSpace(raw []byte) []byte { return bytes.TrimSpace(raw) }
func Extract(outputRaw json.RawMessage) (string, []Part, bool) {
	outputRaw = bytesTrimSpace(outputRaw)
	if len(outputRaw) == 0 || string(outputRaw) == "null" {
		return "", nil, false
	}

	var outputString string
	if err := kitutil.Unmarshal(outputRaw, &outputString); err == nil {
		if isToolOutputImageDataURL(outputString) {
			return toolOutputMediaMarker, []Part{toolOutputImagePart(outputString)}, true
		}

		nested, ok := decodeToolOutputJSON([]byte(outputString))
		if !ok {
			return "", nil, false
		}
		rewritten, media, changed := rewriteToolOutputMediaValue(nested)
		if !changed {
			return "", nil, false
		}
		encoded, err := kitutil.Marshal(rewritten)
		if err != nil {
			return "", nil, false
		}
		return string(encoded), media, true
	}

	value, ok := decodeToolOutputJSON(outputRaw)
	if !ok {
		return "", nil, false
	}
	rewritten, media, changed := rewriteToolOutputMediaValue(value)
	if !changed {
		return "", nil, false
	}
	encoded, err := kitutil.Marshal(rewritten)
	if err != nil {
		return "", nil, false
	}
	return string(encoded), media, true
}

func decodeToolOutputJSON(raw []byte) (any, bool) {
	if !kitutil.Valid(raw) {
		return nil, false
	}
	var value any
	if err := kitutil.UnmarshalUseNumber(raw, &value); err != nil {
		return nil, false
	}
	return value, true
}

func rewriteToolOutputMediaValue(value any) (any, []Part, bool) {
	switch typed := value.(type) {
	case []any:
		var media []Part
		changed := false
		for i, item := range typed {
			rewritten, itemMedia, itemChanged := rewriteToolOutputMediaValue(item)
			if !itemChanged {
				continue
			}
			typed[i] = rewritten
			media = append(media, itemMedia...)
			changed = true
		}
		return typed, media, changed
	case map[string]any:
		if imageURL, ok := recognizedToolOutputImageURL(typed); ok {
			return map[string]any{
				"type": "input_text",
				"text": toolOutputMediaMarker,
			}, []Part{toolOutputImagePart(imageURL)}, true
		}

		content, ok := typed["content"]
		if !ok {
			return typed, nil, false
		}
		rewritten, media, changed := rewriteToolOutputMediaValue(content)
		if !changed {
			return typed, nil, false
		}
		typed["content"] = rewritten
		return typed, media, true
	default:
		return value, nil, false
	}
}

func recognizedToolOutputImageURL(value map[string]any) (string, bool) {
	partType, _ := value["type"].(string)
	if partType != "input_image" && partType != "image_url" {
		return "", false
	}

	switch imageURL := value["image_url"].(type) {
	case string:
		return imageURL, strings.TrimSpace(imageURL) != ""
	case map[string]any:
		url, _ := imageURL["url"].(string)
		return url, strings.TrimSpace(url) != ""
	default:
		return "", false
	}
}

func isToolOutputImageDataURL(value string) bool {
	const prefix = "data:image/"
	const separator = ";base64,"
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	separatorIndex := strings.Index(value[len(prefix):], separator)
	if separatorIndex <= 0 {
		return false
	}
	payloadIndex := len(prefix) + separatorIndex + len(separator)
	return payloadIndex < len(value)
}

func toolOutputImagePart(imageURL string) Part {
	return Part{
		Type:     "image_url",
		ImageURL: &ImageReference{URL: imageURL},
	}
}
