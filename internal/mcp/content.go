package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// MCPImage is one image returned by a tool call, already decoded.
type MCPImage struct {
	Data []byte
	MIME string
}

// parseCallResult splits an MCP tools/call result into the text the model
// reads, the images it can be shown, and the server's error flag.
//
// Resource links and embedded resources become text: a link's URI is what the
// model reads next, and a text resource is the answer itself; a binary one is
// named, not pasted. Anything we cannot carry — audio — is still counted in
// a trailing notice rather than silently omitted, so the model does not treat
// the text as the whole answer. An image that fails to decode counts as
// dropped for the same reason: claiming to have forwarded it would be worse
// than admitting it is gone.
func parseCallResult(raw json.RawMessage) (text string, images []MCPImage, isError bool, err error) {
	var result struct {
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text,omitempty"`
			Data     string `json:"data,omitempty"`
			MimeType string `json:"mimeType,omitempty"`
			// resource_link
			URI         string `json:"uri,omitempty"`
			Name        string `json:"name,omitempty"`
			Description string `json:"description,omitempty"`
			// resource (embedded)
			Resource *struct {
				URI      string `json:"uri"`
				MimeType string `json:"mimeType,omitempty"`
				Text     string `json:"text,omitempty"`
				Blob     string `json:"blob,omitempty"`
			} `json:"resource,omitempty"`
		} `json:"content"`
		// StructuredContent arrived with protocol revision 2025-06-18. The
		// spec asks servers to mirror it into a text item, but the ones that
		// do not used to reach the model as an empty result.
		StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
		IsError           bool            `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return string(raw), nil, false, nil // unparseable: hand back what came
	}

	var b strings.Builder
	dropped := 0
	for _, item := range result.Content {
		switch item.Type {
		case "text":
			b.WriteString(item.Text)
		case "image":
			img, ok := decodeImage(item.Data, item.MimeType)
			if !ok {
				dropped++
				continue
			}
			images = append(images, img)
		case "resource_link":
			b.WriteString(resourceLinkText(item.Name, item.URI, item.MimeType, item.Description))
		case "resource":
			if item.Resource == nil {
				dropped++
				continue
			}
			r := item.Resource
			b.WriteString(embeddedResourceText(r.URI, r.MimeType, r.Text, base64.StdEncoding.DecodedLen(len(r.Blob))))
		default:
			dropped++
		}
	}
	out := b.String()
	// Only when the server sent no readable text at all — otherwise both
	// halves say the same thing and one of them is wasted context.
	if strings.TrimSpace(out) == "" && len(result.StructuredContent) > 0 {
		out = string(result.StructuredContent)
	}
	if dropped > 0 {
		out += droppedNotice(dropped)
	}
	return out, images, result.IsError, nil
}

// resourceLinkText is how a server says "read this next": the model needs the
// URI. Shared by the stdio and remote clients.
func resourceLinkText(name, uri, mime, description string) string {
	s := fmt.Sprintf("\n[resource link: %s <%s>", name, uri)
	if mime != "" {
		s += " " + mime
	}
	if description != "" {
		s += " — " + description
	}
	return s + "]"
}

// embeddedResourceText carries a text resource whole and names a binary one.
func embeddedResourceText(uri, mime, text string, blobBytes int) string {
	if text != "" {
		return fmt.Sprintf("\n[resource %s]\n%s", uri, text)
	}
	return fmt.Sprintf("\n[binary resource %s %s, %d bytes — not shown]", uri, mime, blobBytes)
}

func droppedNotice(n int) string {
	return fmt.Sprintf("\n[orchestra: dropped %d non-text content item(s); text, images and resources are forwarded, other kinds are not]", n)
}

// decodeImage turns a base64 content item into bytes. MIME defaults to PNG,
// which is what every screenshot-producing server emits.
func decodeImage(data, mime string) (MCPImage, bool) {
	data = strings.TrimSpace(data)
	if data == "" {
		return MCPImage{}, false
	}
	// Some servers send a full data URI instead of bare base64.
	if i := strings.Index(data, ";base64,"); i >= 0 {
		if mime == "" {
			mime = strings.TrimPrefix(data[:i], "data:")
		}
		data = data[i+len(";base64,"):]
	}
	bytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil || len(bytes) == 0 {
		return MCPImage{}, false
	}
	if strings.TrimSpace(mime) == "" {
		mime = "image/png"
	}
	return MCPImage{Data: bytes, MIME: mime}, true
}

// knownProtocolVersions are the MCP revisions this client understands, oldest
// first. The server picks; we only need to know whether we recognise it.
var knownProtocolVersions = []string{"2024-11-05", "2025-03-26", "2025-06-18"}

// negotiateProtocolVersion resolves the revision to use from the server's
// reply to initialize.
//
// A server that answers with an older revision is the normal case and the
// reason to negotiate at all: offering 2025-06-18 must not break the servers
// that only speak 2024-11-05. A server ahead of us is accepted too — being
// newer is far likelier than being broken, and the request shapes we send are
// unchanged across these revisions — but it is reported as unrecognised so a
// caller can say so.
func negotiateProtocolVersion(serverVersion string) (version string, known bool) {
	v := strings.TrimSpace(serverVersion)
	if v == "" {
		// Pre-negotiation servers omit the field entirely.
		return knownProtocolVersions[0], true
	}
	for _, k := range knownProtocolVersions {
		if v == k {
			return v, true
		}
	}
	return v, false
}
