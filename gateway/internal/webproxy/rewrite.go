package webproxy

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var (
	cssURLPattern    = regexp.MustCompile(`(?i)url\(\s*([^)]*?)\s*\)`)
	cssImportPattern = regexp.MustCompile(`(?i)(@import\s+)(["'])([^"']+)(["'])`)
)

func rewriteHTML(body []byte, target *url.URL) ([]byte, error) {
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	base := *target
	if discovered := findBaseURL(document, target); discovered != nil {
		base = *discovered
	}
	rewriteNode(document, &base)
	injectBootstrap(document, target)
	var output bytes.Buffer
	if err := html.Render(&output, document); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func findBaseURL(node *html.Node, current *url.URL) *url.URL {
	if node.Type == html.ElementNode && strings.EqualFold(node.Data, "base") {
		for _, attribute := range node.Attr {
			if strings.EqualFold(attribute.Key, "href") {
				if resolved, err := current.Parse(attribute.Val); err == nil && (resolved.Scheme == "http" || resolved.Scheme == "https") {
					return resolved
				}
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findBaseURL(child, current); found != nil {
			return found
		}
	}
	return nil
}

func rewriteNode(node *html.Node, base *url.URL) {
	if node.Type == html.ElementNode {
		if strings.EqualFold(node.Data, "meta") && metaBlocksProxy(node) {
			node.Type = html.CommentNode
			node.Data = "OWU removed an upstream browser policy meta tag"
			node.Attr = nil
		}
		if strings.EqualFold(node.Data, "meta") && metaNameValue(node, "referrer") {
			setAttribute(node, "content", "same-origin")
		}
		for index := range node.Attr {
			attribute := &node.Attr[index]
			key := strings.ToLower(attribute.Key)
			switch key {
			case "href", "src", "action", "formaction", "poster", "data", "cite", "background", "xlink:href", "data-src", "data-href", "data-url", "data-original", "data-lazy-src", "data-background":
				attribute.Val = rewriteReference(attribute.Val, base)
			case "srcset", "imagesrcset", "data-srcset":
				attribute.Val = rewriteSrcset(attribute.Val, base)
			case "ping":
				attribute.Val = rewriteURLList(attribute.Val, base)
			case "style":
				attribute.Val = string(rewriteCSS([]byte(attribute.Val), base))
			case "content":
				if strings.EqualFold(node.Data, "meta") && metaHTTPValue(node, "refresh") {
					attribute.Val = rewriteRefresh(attribute.Val, base)
				}
			}
		}
		node.Attr = removeAttribute(node.Attr, "integrity")
	}
	if node.Type == html.ElementNode && strings.EqualFold(node.Data, "style") {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				child.Data = string(rewriteCSS([]byte(child.Data), base))
			}
		}
	}
	if node.Type == html.ElementNode && strings.EqualFold(node.Data, "script") && attributeEquals(node, "type", "importmap") {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.TextNode {
				child.Data = rewriteImportMap(child.Data, base)
			}
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		rewriteNode(child, base)
	}
}

func attributeEquals(node *html.Node, key, expected string) bool {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, key) && strings.EqualFold(strings.TrimSpace(attribute.Val), expected) {
			return true
		}
	}
	return false
}

func rewriteImportMap(source string, base *url.URL) string {
	var document map[string]any
	if err := json.Unmarshal([]byte(source), &document); err != nil {
		return source
	}

	changed := false
	if imports, ok := document["imports"].(map[string]any); ok {
		changed = rewriteImportAddresses(imports, base) || changed
	}
	if scopes, ok := document["scopes"].(map[string]any); ok {
		rewrittenScopes := make(map[string]any, len(scopes))
		for scope, value := range scopes {
			rewrittenScope := rewriteImportAddress(scope, base)
			if rewrittenScope != scope {
				changed = true
			}
			if imports, ok := value.(map[string]any); ok {
				changed = rewriteImportAddresses(imports, base) || changed
			}
			rewrittenScopes[rewrittenScope] = value
		}
		document["scopes"] = rewrittenScopes
	}
	if integrity, ok := document["integrity"].(map[string]any); ok {
		rewrittenIntegrity := make(map[string]any, len(integrity))
		for resource, value := range integrity {
			rewrittenResource := rewriteImportAddress(resource, base)
			if rewrittenResource != resource {
				changed = true
			}
			rewrittenIntegrity[rewrittenResource] = value
		}
		document["integrity"] = rewrittenIntegrity
	}
	if !changed {
		return source
	}
	rewritten, err := json.Marshal(document)
	if err != nil {
		return source
	}
	return string(rewritten)
}

func rewriteImportAddresses(imports map[string]any, base *url.URL) bool {
	changed := false
	for specifier, value := range imports {
		address, ok := value.(string)
		if !ok {
			continue
		}
		rewritten := rewriteImportAddress(address, base)
		if rewritten != address {
			imports[specifier] = rewritten
			changed = true
		}
	}
	return changed
}

func rewriteImportAddress(value string, base *url.URL) string {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if !(strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) {
		return value
	}
	return rewriteReference(value, base)
}

func rewriteReference(value string, base *url.URL) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return value
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"data:", "blob:", "javascript:", "mailto:", "tel:", "about:"} {
		if strings.HasPrefix(lower, prefix) {
			return value
		}
	}
	resolved, err := base.Parse(trimmed)
	if err != nil || (resolved.Scheme != "http" && resolved.Scheme != "https") {
		return value
	}
	return proxyURL(resolved)
}

// rewriteHLSManifest rewrites every network reference in an HLS media or
// multivariant playlist through the canonical /browse/{origin}/ route. It is
// deliberately line based: HLS URI lines and attribute lists are not CSV, and
// a signed URI inside quotes may itself contain commas.
//
// The function preserves the input's line endings and all non-URI text. Each
// upstream body is rewritten once before Nginx stores the resulting response;
// a cache hit is served directly and does not re-enter this function.
func rewriteHLSManifest(body []byte, base *url.URL) []byte {
	if len(body) == 0 || base == nil {
		return append([]byte(nil), body...)
	}

	input := string(body)
	var output strings.Builder
	output.Grow(len(input) + len(input)/4)

	for position := 0; position < len(input); {
		lineEnd := position
		for lineEnd < len(input) && input[lineEnd] != '\r' && input[lineEnd] != '\n' {
			lineEnd++
		}
		output.WriteString(rewriteHLSLine(input[position:lineEnd], base))

		if lineEnd == len(input) {
			break
		}
		if input[lineEnd] == '\r' && lineEnd+1 < len(input) && input[lineEnd+1] == '\n' {
			output.WriteString("\r\n")
			position = lineEnd + 2
		} else {
			output.WriteByte(input[lineEnd])
			position = lineEnd + 1
		}
	}

	return []byte(output.String())
}

func rewriteHLSLine(line string, base *url.URL) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return line
	}
	if !strings.HasPrefix(trimmed, "#") {
		return rewriteManifestReference(line, base)
	}
	if !strings.HasPrefix(strings.ToUpper(trimmed), "#EXT-X-") {
		return line
	}
	return rewriteHLSURIAttributes(line, base)
}

func rewriteHLSURIAttributes(line string, base *url.URL) string {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return line
	}

	var output strings.Builder
	output.Grow(len(line) + len(line)/4)
	lastWritten := 0
	position := colon + 1

	for position < len(line) {
		for position < len(line) && (line[position] == ',' || isASCIIWhitespace(line[position])) {
			position++
		}
		if position >= len(line) {
			break
		}

		nameStart := position
		for position < len(line) && line[position] != '=' && line[position] != ',' {
			position++
		}
		if position >= len(line) || line[position] != '=' {
			for position < len(line) && line[position] != ',' {
				position++
			}
			continue
		}

		name := strings.TrimSpace(line[nameStart:position])
		position++
		for position < len(line) && isASCIIWhitespace(line[position]) {
			position++
		}
		if position >= len(line) {
			break
		}

		isURI := strings.EqualFold(name, "URI") || strings.EqualFold(name, "SERVER-URI")
		if line[position] == '"' || line[position] == '\'' {
			quote := line[position]
			valueStart := position + 1
			valueEnd := valueStart
			for valueEnd < len(line) && line[valueEnd] != quote {
				valueEnd++
			}
			if valueEnd >= len(line) {
				break
			}
			if isURI {
				rewritten := rewriteManifestReference(line[valueStart:valueEnd], base)
				if rewritten != line[valueStart:valueEnd] {
					output.WriteString(line[lastWritten:valueStart])
					output.WriteString(rewritten)
					lastWritten = valueEnd
				}
			}
			position = valueEnd + 1
			continue
		}

		valueStart := position
		for position < len(line) && line[position] != ',' {
			position++
		}
		if isURI {
			rewritten := rewriteManifestReference(line[valueStart:position], base)
			if rewritten != line[valueStart:position] {
				output.WriteString(line[lastWritten:valueStart])
				output.WriteString(rewritten)
				lastWritten = position
			}
		}
	}

	if lastWritten == 0 {
		return line
	}
	output.WriteString(line[lastWritten:])
	return output.String()
}

// rewriteManifestReference is rewriteReference with media-specific whitespace
// preservation. Manifest input is always interpreted in the target document's
// URL space: a target is allowed to have a real /browse/<token>/... path, so a
// browse-shaped value cannot be trusted as an already-proxied route here.
func rewriteManifestReference(value string, base *url.URL) string {
	start, end := trimASCIIWhitespaceBounds(value)
	if start == end || base == nil {
		return value
	}
	trimmed := value[start:end]
	rewritten := rewriteReference(trimmed, base)
	if rewritten == trimmed {
		return value
	}
	return value[:start] + rewritten + value[end:]
}

func trimASCIIWhitespaceBounds(value string) (int, int) {
	start := 0
	for start < len(value) && isASCIIWhitespace(value[start]) {
		start++
	}
	end := len(value)
	for end > start && isASCIIWhitespace(value[end-1]) {
		end--
	}
	return start, end
}

// rewriteDASHManifest performs a formatting-preserving rewrite of the URL
// surfaces defined by DASH: BaseURL/Location text, xlink/xml base links, and
// SegmentTemplate/SegmentURL/Initialization URL attributes. Absolute,
// scheme-relative, and root-relative references are always proxied. Relative
// references remain relative when the document has BaseURL/xml:base hierarchy,
// so the DASH inheritance rules keep working after its parent BaseURL is
// rewritten.
func rewriteDASHManifest(body []byte, base *url.URL) ([]byte, error) {
	if len(body) == 0 || base == nil {
		return append([]byte(nil), body...), nil
	}
	if err := validateXML(body); err != nil {
		return nil, err
	}

	input := string(body)
	hasBaseHierarchy := dashHasBaseHierarchy(input)
	stack := make([]string, 0, 8)
	var output strings.Builder
	output.Grow(len(input) + len(input)/5)

	for position := 0; position < len(input); {
		markup := strings.IndexByte(input[position:], '<')
		if markup < 0 {
			text := input[position:]
			if len(stack) > 0 && isDASHURLTextElement(stack[len(stack)-1]) {
				text = rewriteDASHText(text, base)
			}
			output.WriteString(text)
			break
		}
		markup += position
		text := input[position:markup]
		if len(stack) > 0 && isDASHURLTextElement(stack[len(stack)-1]) {
			text = rewriteDASHText(text, base)
		}
		output.WriteString(text)

		switch {
		case strings.HasPrefix(input[markup:], "<!--"):
			end := strings.Index(input[markup+4:], "-->")
			if end < 0 {
				return nil, io.ErrUnexpectedEOF
			}
			end += markup + 7
			output.WriteString(input[markup:end])
			position = end
			continue
		case strings.HasPrefix(input[markup:], "<![CDATA["):
			end := strings.Index(input[markup+9:], "]]>")
			if end < 0 {
				return nil, io.ErrUnexpectedEOF
			}
			end += markup + 12
			output.WriteString(input[markup:end])
			position = end
			continue
		case strings.HasPrefix(input[markup:], "<?"):
			end := strings.Index(input[markup+2:], "?>")
			if end < 0 {
				return nil, io.ErrUnexpectedEOF
			}
			end += markup + 4
			output.WriteString(input[markup:end])
			position = end
			continue
		}

		tagEnd := findXMLTagEnd(input, markup)
		if tagEnd < 0 {
			return nil, io.ErrUnexpectedEOF
		}
		tag := input[markup : tagEnd+1]
		if strings.HasPrefix(tag, "<!") {
			output.WriteString(tag)
			position = tagEnd + 1
			continue
		}

		name, closing, selfClosing := xmlTagName(tag)
		if closing {
			output.WriteString(tag)
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			position = tagEnd + 1
			continue
		}

		output.WriteString(rewriteDASHStartTag(tag, name, base, hasBaseHierarchy))
		if name != "" && !selfClosing {
			stack = append(stack, name)
		}
		position = tagEnd + 1
	}

	rewritten := []byte(output.String())
	if err := validateXML(rewritten); err != nil {
		return nil, err
	}
	return rewritten, nil
}

func validateXML(body []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		if _, err := decoder.Token(); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func dashHasBaseHierarchy(input string) bool {
	lower := strings.ToLower(input)
	return strings.Contains(lower, "<baseurl") || strings.Contains(lower, ":baseurl") || strings.Contains(lower, "xml:base=")
}

func findXMLTagEnd(input string, start int) int {
	quote := byte(0)
	brackets := 0
	for position := start + 1; position < len(input); position++ {
		switch input[position] {
		case '\'', '"':
			if quote == 0 {
				quote = input[position]
			} else if quote == input[position] {
				quote = 0
			}
		case '[':
			if quote == 0 {
				brackets++
			}
		case ']':
			if quote == 0 && brackets > 0 {
				brackets--
			}
		case '>':
			if quote == 0 && brackets == 0 {
				return position
			}
		}
	}
	return -1
}

func xmlTagName(tag string) (name string, closing, selfClosing bool) {
	position := 1
	for position < len(tag) && isASCIIWhitespace(tag[position]) {
		position++
	}
	if position < len(tag) && tag[position] == '/' {
		closing = true
		position++
		for position < len(tag) && isASCIIWhitespace(tag[position]) {
			position++
		}
	}
	start := position
	for position < len(tag) && !isASCIIWhitespace(tag[position]) && tag[position] != '/' && tag[position] != '>' {
		position++
	}
	name = strings.ToLower(tag[start:position])
	if colon := strings.LastIndexByte(name, ':'); colon >= 0 {
		name = name[colon+1:]
	}
	end := len(tag) - 2
	for end >= 0 && isASCIIWhitespace(tag[end]) {
		end--
	}
	selfClosing = end >= 0 && tag[end] == '/'
	return name, closing, selfClosing
}

func isDASHURLTextElement(name string) bool {
	switch name {
	case "baseurl", "location", "patchlocation":
		return true
	default:
		return false
	}
}

func rewriteDASHText(raw string, base *url.URL) string {
	decoded, err := decodeXMLText(raw)
	if err != nil {
		return raw
	}
	rewritten := rewriteDASHExternalReference(decoded, base)
	if rewritten == decoded {
		return raw
	}
	return escapeXMLText(rewritten)
}

func rewriteDASHStartTag(tag, element string, base *url.URL, hasBaseHierarchy bool) string {
	if element == "" {
		return tag
	}
	nameEnd := 1
	for nameEnd < len(tag) && !isASCIIWhitespace(tag[nameEnd]) && tag[nameEnd] != '>' && tag[nameEnd] != '/' {
		nameEnd++
	}

	var output strings.Builder
	output.Grow(len(tag) + len(tag)/5)
	lastWritten := 0
	position := nameEnd
	for position < len(tag)-1 {
		for position < len(tag)-1 && isASCIIWhitespace(tag[position]) {
			position++
		}
		if position >= len(tag)-1 || tag[position] == '/' || tag[position] == '>' {
			break
		}
		attributeStart := position
		for position < len(tag)-1 && !isASCIIWhitespace(tag[position]) && tag[position] != '=' && tag[position] != '>' {
			position++
		}
		attributeName := tag[attributeStart:position]
		for position < len(tag)-1 && isASCIIWhitespace(tag[position]) {
			position++
		}
		if position >= len(tag)-1 || tag[position] != '=' {
			continue
		}
		position++
		for position < len(tag)-1 && isASCIIWhitespace(tag[position]) {
			position++
		}
		if position >= len(tag)-1 || (tag[position] != '"' && tag[position] != '\'') {
			continue
		}
		quote := tag[position]
		valueStart := position + 1
		valueEnd := valueStart
		for valueEnd < len(tag)-1 && tag[valueEnd] != quote {
			valueEnd++
		}
		if valueEnd >= len(tag)-1 {
			break
		}

		if isDASHURLAttribute(element, attributeName) {
			decoded, err := decodeXMLAttribute(tag[valueStart:valueEnd], quote)
			if err == nil {
				rewritten := decoded
				if hasBaseHierarchy {
					rewritten = rewriteDASHExternalReference(decoded, base)
				} else {
					rewritten = rewriteManifestReference(decoded, base)
				}
				if rewritten != decoded {
					output.WriteString(tag[lastWritten:valueStart])
					output.WriteString(escapeXMLAttribute(rewritten, quote))
					lastWritten = valueEnd
				}
			}
		}
		position = valueEnd + 1
	}
	if lastWritten == 0 {
		return tag
	}
	output.WriteString(tag[lastWritten:])
	return output.String()
}

func isDASHURLAttribute(element, attributeName string) bool {
	local := strings.ToLower(attributeName)
	if colon := strings.LastIndexByte(local, ':'); colon >= 0 {
		local = local[colon+1:]
	}
	if local == "href" || strings.EqualFold(attributeName, "xml:base") {
		return true
	}
	switch element {
	case "segmenttemplate":
		return local == "media" || local == "initialization" || local == "index"
	case "segmenturl":
		return local == "media" || local == "index"
	case "initialization", "representationindex":
		return local == "sourceurl"
	default:
		return false
	}
}

func rewriteDASHExternalReference(value string, base *url.URL) string {
	start, end := trimASCIIWhitespaceBounds(value)
	if start == end {
		return value
	}
	trimmed := value[start:end]
	lower := strings.ToLower(trimmed)
	if !(strings.HasPrefix(trimmed, "/") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")) {
		return value
	}
	return rewriteManifestReference(value, base)
}

func decodeXMLText(raw string) (string, error) {
	var wrapper struct {
		Value string `xml:",chardata"`
	}
	err := xml.Unmarshal([]byte("<owu>"+raw+"</owu>"), &wrapper)
	return wrapper.Value, err
}

func decodeXMLAttribute(raw string, quote byte) (string, error) {
	var wrapper struct {
		Value string `xml:"value,attr"`
	}
	markup := "<owu value=" + string(quote) + raw + string(quote) + "/>"
	err := xml.Unmarshal([]byte(markup), &wrapper)
	return wrapper.Value, err
}

func escapeXMLText(value string) string {
	var output bytes.Buffer
	if err := xml.EscapeText(&output, []byte(value)); err != nil {
		return value
	}
	return output.String()
}

func escapeXMLAttribute(value string, quote byte) string {
	escaped := escapeXMLText(value)
	if quote == '"' {
		return strings.ReplaceAll(escaped, "\"", "&#34;")
	}
	return strings.ReplaceAll(escaped, "'", "&#39;")
}

func rewriteSrcset(value string, base *url.URL) string {
	candidates := parseSrcset(value)
	rewritten := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		entry := rewriteReference(candidate.url, base)
		if candidate.descriptor != "" {
			entry += " " + candidate.descriptor
		}
		rewritten = append(rewritten, entry)
	}
	return strings.Join(rewritten, ", ")
}

type srcsetCandidate struct {
	url        string
	descriptor string
}

// parseSrcset follows the URL/descriptor boundaries from the HTML srcset
// parsing algorithm. In particular, commas inside a URL are data, not
// candidate separators. This is common in Cloudflare image-resizing URLs such
// as /cdn-cgi/image/q=78,scq=50,width=188/image.png.
func parseSrcset(value string) []srcsetCandidate {
	candidates := make([]srcsetCandidate, 0, 2)
	for position := 0; position < len(value); {
		for position < len(value) && (isASCIIWhitespace(value[position]) || value[position] == ',') {
			position++
		}
		if position >= len(value) {
			break
		}

		urlStart := position
		for position < len(value) && !isASCIIWhitespace(value[position]) {
			position++
		}
		rawURL := value[urlStart:position]
		urlValue := strings.TrimRight(rawURL, ",")
		hadTrailingComma := len(urlValue) != len(rawURL)

		descriptor := ""
		if !hadTrailingComma {
			for position < len(value) && isASCIIWhitespace(value[position]) {
				position++
			}
			descriptorStart := position
			parentheses := 0
			for position < len(value) {
				switch value[position] {
				case '(':
					parentheses++
				case ')':
					if parentheses > 0 {
						parentheses--
					}
				case ',':
					if parentheses == 0 {
						descriptor = strings.TrimSpace(value[descriptorStart:position])
						position++
						goto candidateComplete
					}
				}
				position++
			}
			descriptor = strings.TrimSpace(value[descriptorStart:position])
		}

	candidateComplete:
		if urlValue != "" {
			candidates = append(candidates, srcsetCandidate{url: urlValue, descriptor: descriptor})
		}
	}
	return candidates
}

func isASCIIWhitespace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\f', '\r':
		return true
	default:
		return false
	}
}

func rewriteURLList(value string, base *url.URL) string {
	parts := strings.Fields(value)
	for index := range parts {
		parts[index] = rewriteReference(parts[index], base)
	}
	return strings.Join(parts, " ")
}

func rewriteCSS(body []byte, base *url.URL) []byte {
	text := string(body)
	text = cssURLPattern.ReplaceAllStringFunc(text, func(match string) string {
		inside := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match[strings.Index(match, "("):], "("), ")"))
		quote := ""
		if len(inside) >= 2 && ((inside[0] == '\'' && inside[len(inside)-1] == '\'') || (inside[0] == '"' && inside[len(inside)-1] == '"')) {
			quote = inside[:1]
			inside = inside[1 : len(inside)-1]
		}
		rewritten := rewriteReference(inside, base)
		return "url(" + quote + rewritten + quote + ")"
	})
	text = cssImportPattern.ReplaceAllStringFunc(text, func(match string) string {
		groups := cssImportPattern.FindStringSubmatch(match)
		return groups[1] + groups[2] + rewriteReference(groups[3], base) + groups[4]
	})
	return []byte(text)
}

func metaBlocksProxy(node *html.Node) bool {
	return metaHTTPValue(node, "content-security-policy") || metaHTTPValue(node, "content-security-policy-report-only")
}

func metaHTTPValue(node *html.Node, expected string) bool {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, "http-equiv") && strings.EqualFold(strings.TrimSpace(attribute.Val), expected) {
			return true
		}
	}
	return false
}

func metaNameValue(node *html.Node, expected string) bool {
	for _, attribute := range node.Attr {
		if strings.EqualFold(attribute.Key, "name") && strings.EqualFold(strings.TrimSpace(attribute.Val), expected) {
			return true
		}
	}
	return false
}

func setAttribute(node *html.Node, key, value string) {
	for index := range node.Attr {
		if strings.EqualFold(node.Attr[index].Key, key) {
			node.Attr[index].Val = value
			return
		}
	}
	node.Attr = append(node.Attr, html.Attribute{Key: key, Val: value})
}

func removeAttribute(attributes []html.Attribute, key string) []html.Attribute {
	result := attributes[:0]
	for _, attribute := range attributes {
		if !strings.EqualFold(attribute.Key, key) {
			result = append(result, attribute)
		}
	}
	return result
}

func injectBootstrap(document *html.Node, target *url.URL) {
	head := findElement(document, "head")
	if head == nil {
		return
	}
	script := &html.Node{Type: html.ElementNode, Data: "script", Attr: []html.Attribute{{Key: "data-owu", Val: "bootstrap"}}}
	script.AppendChild(&html.Node{Type: html.TextNode, Data: clientBootstrap(target)})
	if head.FirstChild == nil {
		head.AppendChild(script)
	} else {
		head.InsertBefore(script, head.FirstChild)
	}
}

func findElement(node *html.Node, name string) *html.Node {
	if node.Type == html.ElementNode && strings.EqualFold(node.Data, name) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func clientBootstrap(target *url.URL) string {
	baseJSON, _ := json.Marshal(target.String())
	originJSON, _ := json.Marshal(target.Scheme + "://" + target.Host)
	cookiePrefixJSON, _ := json.Marshal(cookiePrefix(encodeOrigin(target)))
	return `(function(){
"use strict";
const targetBase=` + string(baseJSON) + `;
const targetOrigin=` + string(originJSON) + `;
const cookiePrefix=` + string(cookiePrefixJSON) + `;
const browsePrefix="/browse/";
const socketPrefix="/socket/";
const virtualOriginParam="__owu_origin_v1";
const encodeOrigin=(origin)=>{const bytes=new TextEncoder().encode(origin);let raw="";for(const byte of bytes)raw+=String.fromCharCode(byte);return btoa(raw).replace(/\+/g,"-").replace(/\//g,"_").replace(/=+$/g,"");};
const decodeOrigin=(token)=>{let raw=String(token).replace(/-/g,"+").replace(/_/g,"/");while(raw.length%4)raw+="=";const binary=atob(raw);const bytes=Uint8Array.from(binary,char=>char.charCodeAt(0));return new TextDecoder().decode(bytes);};
const targetURL=(origin,path,search="",hash="")=>{const target=new URL(origin);target.pathname=path||"/";target.search=search;target.hash=hash;return target;};
const passthrough=/^(?:data|blob|javascript|mailto|tel|about):/i;
const targetFromProxyURL=(value)=>{try{const parsed=new URL(value,location.origin);if(parsed.origin!==location.origin||!parsed.pathname.startsWith(browsePrefix))return null;const remainder=parsed.pathname.slice(browsePrefix.length);const slash=remainder.indexOf("/");const token=slash<0?remainder:remainder.slice(0,slash);const path=slash<0?"/":remainder.slice(slash);const origin=decodeOrigin(token);return targetURL(origin,path,parsed.search,parsed.hash);}catch{return null;}};
const socketProxyURL=(value)=>{try{const parsed=new URL(value,location.origin);if(parsed.origin!==location.origin||!parsed.pathname.startsWith(socketPrefix))return null;const remainder=parsed.pathname.slice(socketPrefix.length);const slash=remainder.indexOf("/");const token=slash<0?remainder:remainder.slice(0,slash);const origin=new URL(decodeOrigin(token));return origin.protocol==="ws:"||origin.protocol==="wss:"?parsed:null;}catch{return null;}};
const decodeQueryPart=(value)=>decodeURIComponent(String(value).replace(/\+/g," "));
const splitVirtualURL=(value)=>{try{const parsed=new URL(value,location.origin);if(parsed.origin!==location.origin)return null;const raw=parsed.search.startsWith("?")?parsed.search.slice(1):parsed.search;const parts=raw.split("&");for(let index=parts.length-1;index>=0;index--){const separator=parts[index].indexOf("=");if(separator<0)continue;let key,token;try{key=decodeQueryPart(parts[index].slice(0,separator));token=decodeQueryPart(parts[index].slice(separator+1));}catch{continue;}if(key!==virtualOriginParam||!token)continue;let origin;try{origin=decodeOrigin(token);const checked=new URL(origin);if(checked.protocol!=="http:"&&checked.protocol!=="https:")continue;origin=checked.origin;}catch{continue;}parts.splice(index,1);return{target:targetURL(origin,parsed.pathname,parts.length?"?"+parts.join("&"):"",parsed.hash),token};}return null;}catch{return null;}};
const targetFromVirtualURL=(value)=>splitVirtualURL(value)?.target||null;
const liveTargetBase=()=>targetFromProxyURL(document.baseURI)||targetFromProxyURL(location.href)||targetFromVirtualURL(location.href)||new URL(targetBase);
const resolveTarget=(value)=>{const text=String(value);const proxyCandidate=new URL(text,location.href);const proxyTarget=targetFromProxyURL(proxyCandidate.href);if(proxyTarget)return proxyTarget;const virtualTarget=targetFromVirtualURL(proxyCandidate.href);if(virtualTarget)return virtualTarget;const socketTarget=socketProxyURL(proxyCandidate.href);if(socketTarget)return socketTarget;const parsed=new URL(text,liveTargetBase());if(parsed.origin===location.origin)return targetURL(targetOrigin,parsed.pathname,parsed.search,parsed.hash);return parsed;};
const proxify=(value)=>{if(value==null||passthrough.test(String(value)))return value;try{const parsed=resolveTarget(value);if(parsed.origin===location.origin&&(parsed.pathname.startsWith(browsePrefix)||parsed.pathname.startsWith(socketPrefix)))return parsed.href;if(parsed.protocol!=="http:"&&parsed.protocol!=="https:")return value;return location.origin+browsePrefix+encodeOrigin(parsed.origin)+parsed.pathname+parsed.search+parsed.hash;}catch{return value;}};
const virtualize=(value)=>{try{const parsed=resolveTarget(value);if(parsed.protocol!=="http:"&&parsed.protocol!=="https:")return value;const marker=encodeURIComponent(virtualOriginParam)+"="+encodeURIComponent(encodeOrigin(parsed.origin));return location.origin+parsed.pathname+parsed.search+(parsed.search?"&":"?")+marker+parsed.hash;}catch{return value;}};
const initialProxyTarget=targetFromProxyURL(location.href);if(initialProxyTarget)history.replaceState(history.state,"",virtualize(initialProxyTarget.href));
const cookieOwner=[Document.prototype,typeof HTMLDocument==="function"?HTMLDocument.prototype:null].find(prototype=>prototype&&Object.getOwnPropertyDescriptor(prototype,"cookie"));
if(cookieOwner){const nativeCookie=Object.getOwnPropertyDescriptor(cookieOwner,"cookie");try{Object.defineProperty(cookieOwner,"cookie",{configurable:nativeCookie.configurable,enumerable:nativeCookie.enumerable,get(){const stored=nativeCookie.get.call(this);return String(stored||"").split(/;\s*/).filter(Boolean).flatMap(entry=>{const separator=entry.indexOf("=");const name=(separator<0?entry:entry.slice(0,separator)).trim();return name.startsWith(cookiePrefix)?[name.slice(cookiePrefix.length)+(separator<0?"":entry.slice(separator))]:[];}).join("; ");},set(value){const segments=String(value).split(";");const pair=(segments.shift()||"").trim();const separator=pair.indexOf("=");if(separator<=0)return;let name=pair.slice(0,separator).trim();if(!name.startsWith(cookiePrefix))name=cookiePrefix+name;const attributes=segments.map(segment=>segment.trim()).filter(Boolean).filter(segment=>{const key=segment.split("=",1)[0].trim().toLowerCase();return key!=="domain"&&key!=="path";});attributes.push("Path=/");nativeCookie.set.call(this,name+pair.slice(separator)+(attributes.length?"; "+attributes.join("; "):""));}});}catch{}}
const nativeFetch=window.fetch;if(nativeFetch)window.fetch=function(input,init){if(input instanceof Request)return nativeFetch.call(this,new Request(proxify(input.url),input),init);return nativeFetch.call(this,proxify(input),init);};
const xhrOpen=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(method,url){const args=Array.from(arguments);args[1]=proxify(url);return xhrOpen.apply(this,args);};
if(window.EventSource){const NativeEventSource=window.EventSource;window.EventSource=function(url,config){return new NativeEventSource(proxify(url),config);};window.EventSource.prototype=NativeEventSource.prototype;}
if(window.WebSocket){const NativeWebSocket=window.WebSocket;window.WebSocket=function(value,protocols){const parsed=resolveTarget(value);const wsScheme=location.protocol==="https:"?"wss:":"ws:";const targetScheme=parsed.protocol==="wss:"||parsed.protocol==="https:"?"wss:":"ws:";const endpoint=wsScheme+"//"+location.host+socketPrefix+encodeOrigin(targetScheme+"//"+parsed.host)+parsed.pathname+parsed.search;return protocols===undefined?new NativeWebSocket(endpoint):new NativeWebSocket(endpoint,protocols);};window.WebSocket.prototype=NativeWebSocket.prototype;Object.setPrototypeOf(window.WebSocket,NativeWebSocket);}
const nativeOpen=window.open;window.open=function(url){const args=Array.from(arguments);if(args.length&&args[0]!=null&&String(args[0])!=="")args[0]=virtualize(args[0]);return nativeOpen.apply(this,args);};
const pushState=history.pushState;history.pushState=function(state,title,url){return pushState.call(this,state,title,url==null?url:virtualize(url));};
const replaceState=history.replaceState;history.replaceState=function(state,title,url){return replaceState.call(this,state,title,url==null?url:virtualize(url));};
if("NavigationEvent" in window&&window.navigation&&window.navigation.addEventListener){window.navigation.addEventListener("navigate",event=>{if(!event.canIntercept||event.hashChange||event.downloadRequest!==null)return;try{const rewritten=virtualize(event.destination.url);if(rewritten!==event.destination.url)event.intercept({handler:()=>location.replace(rewritten)});}catch{}});}
if(navigator.sendBeacon){const nativeBeacon=navigator.sendBeacon.bind(navigator);navigator.sendBeacon=function(url,data){return nativeBeacon(proxify(url),data);};}
if(window.Worker){const NativeWorker=window.Worker;window.Worker=function(url,options){return new NativeWorker(proxify(url),options);};window.Worker.prototype=NativeWorker.prototype;Object.setPrototypeOf(window.Worker,NativeWorker);}
if(window.SharedWorker){const NativeSharedWorker=window.SharedWorker;window.SharedWorker=function(url,options){return new NativeSharedWorker(proxify(url),options);};window.SharedWorker.prototype=NativeSharedWorker.prototype;Object.setPrototypeOf(window.SharedWorker,NativeSharedWorker);}
if(navigator.serviceWorker){const serviceWorkerMessage="OWU disables Service Worker registration because proxied sites share one browser origin.";const rejectServiceWorker=()=>Promise.reject(new DOMException(serviceWorkerMessage,"SecurityError"));const serviceWorkerPrototype=Object.getPrototypeOf(navigator.serviceWorker);try{Object.defineProperty(serviceWorkerPrototype,"register",{configurable:true,value:rejectServiceWorker});}catch{}try{Object.defineProperty(navigator.serviceWorker,"register",{configurable:true,value:rejectServiceWorker});}catch{try{navigator.serviceWorker.register=rejectServiceWorker;}catch{}}if(navigator.serviceWorker.getRegistrations)navigator.serviceWorker.getRegistrations().then(registrations=>Promise.all(registrations.map(registration=>registration.unregister()))).catch(()=>{});}
const urlAttributes=new Set(["href","src","action","formaction","poster","data","cite","background","xlink:href","data-src","data-href","data-url","data-original","data-lazy-src","data-background"]);
const parseSrcset=(value)=>{const input=String(value),candidates=[];const isSpace=character=>/[\t\n\f\r ]/.test(character);let position=0;while(position<input.length){while(position<input.length&&(isSpace(input[position])||input[position]===","))position++;if(position>=input.length)break;const urlStart=position;while(position<input.length&&!isSpace(input[position]))position++;const rawURL=input.slice(urlStart,position);const url=rawURL.replace(/,+$/,"");const hadTrailingComma=url.length!==rawURL.length;let descriptor="";if(!hadTrailingComma){while(position<input.length&&isSpace(input[position]))position++;const descriptorStart=position;let parentheses=0;while(position<input.length){const character=input[position];if(character==="(")parentheses++;else if(character===")"&&parentheses>0)parentheses--;else if(character===","&&parentheses===0)break;position++;}descriptor=input.slice(descriptorStart,position).trim();if(position<input.length&&input[position]===",")position++;}if(url)candidates.push({url,descriptor});}return candidates;};
const proxifySrcset=(value)=>parseSrcset(value).map(candidate=>proxify(candidate.url)+(candidate.descriptor?" "+candidate.descriptor:"")).join(", ");
const proxifyStyle=(value)=>String(value).replace(/url\(\s*(["']?)(.*?)\1\s*\)/gi,(match,quote,raw)=>"url("+quote+proxify(raw)+quote+")");
const rewriteAttributeValue=(element,name,value)=>{const lower=String(name).toLowerCase();if(lower==="href"&&(element instanceof HTMLAnchorElement||element instanceof HTMLAreaElement))return virtualize(value);if(urlAttributes.has(lower))return proxify(value);if(lower==="srcset"||lower==="imagesrcset"||lower==="data-srcset")return proxifySrcset(value);if(lower==="ping")return String(value).trim().split(/\s+/).map(proxify).join(" ");if(lower==="style")return proxifyStyle(value);return value;};
const nativeSetAttribute=Element.prototype.setAttribute;Element.prototype.setAttribute=function(name,value){return nativeSetAttribute.call(this,name,rewriteAttributeValue(this,name,value));};
const patchURLProperty=(prototype,name,transform=proxify)=>{try{const descriptor=Object.getOwnPropertyDescriptor(prototype,name);if(!descriptor||!descriptor.set||!descriptor.get)return;Object.defineProperty(prototype,name,{configurable:descriptor.configurable,enumerable:descriptor.enumerable,get:descriptor.get,set(value){descriptor.set.call(this,transform(value));}});}catch{}};
for(const [prototype,name,transform] of [[HTMLAnchorElement.prototype,"href",virtualize],[HTMLAreaElement.prototype,"href",virtualize],[HTMLImageElement.prototype,"src"],[HTMLImageElement.prototype,"srcset",proxifySrcset],[HTMLScriptElement.prototype,"src"],[HTMLLinkElement.prototype,"href"],[HTMLFormElement.prototype,"action"],[HTMLIFrameElement.prototype,"src"],[HTMLSourceElement.prototype,"src"],[HTMLSourceElement.prototype,"srcset",proxifySrcset],[HTMLMediaElement.prototype,"src"]])patchURLProperty(prototype,name,transform);
if(window.CSSStyleSheet&&CSSStyleSheet.prototype.insertRule){const nativeInsertRule=CSSStyleSheet.prototype.insertRule;CSSStyleSheet.prototype.insertRule=function(rule,index){return nativeInsertRule.call(this,proxifyStyle(rule),index);};}
const rewriteElement=(element)=>{if(!(element instanceof Element))return;for(const attribute of Array.from(element.attributes||[])){const rewritten=rewriteAttributeValue(element,attribute.name,attribute.value);if(rewritten!==attribute.value)nativeSetAttribute.call(element,attribute.name,rewritten);}};
const rewriteTree=(node)=>{rewriteElement(node);if(node&&node.querySelectorAll)for(const child of node.querySelectorAll("[href],[src],[action],[formaction],[poster],[data],[cite],[background],[xlink\\:href],[srcset],[imagesrcset],[data-src],[data-href],[data-url],[data-original],[data-lazy-src],[data-background],[data-srcset],[ping],[style]"))rewriteElement(child);};
document.addEventListener("click",event=>{const anchor=event.target&&event.target.closest?event.target.closest("a[href]"):null;if(anchor)nativeSetAttribute.call(anchor,"href",virtualize(anchor.getAttribute("href")));},true);
document.addEventListener("submit",event=>{const form=event.target;if(form instanceof HTMLFormElement&&form.hasAttribute("action"))nativeSetAttribute.call(form,"action",proxify(form.getAttribute("action")));},true);
const observer=new MutationObserver(records=>{for(const record of records){if(record.type==="attributes")rewriteElement(record.target);for(const node of record.addedNodes)rewriteTree(node);}});observer.observe(document.documentElement,{subtree:true,childList:true,attributes:true,attributeFilter:["href","src","action","formaction","poster","data","cite","background","xlink:href","srcset","imagesrcset","data-src","data-href","data-url","data-original","data-lazy-src","data-background","data-srcset","ping","style"]});
rewriteTree(document.documentElement);
window.__OWU__={target:targetBase,proxify,rewrite:rewriteTree};
})();`
}
