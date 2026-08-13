package webproxy

import (
	"bytes"
	"encoding/json"
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
		for index := range node.Attr {
			attribute := &node.Attr[index]
			key := strings.ToLower(attribute.Key)
			switch key {
			case "href", "src", "action", "formaction", "poster", "data", "cite", "background":
				attribute.Val = rewriteReference(attribute.Val, base)
			case "srcset":
				attribute.Val = rewriteSrcset(attribute.Val, base)
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
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		rewriteNode(child, base)
	}
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

func rewriteSrcset(value string, base *url.URL) string {
	parts := strings.Split(value, ",")
	for index, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) > 0 {
			fields[0] = rewriteReference(fields[0], base)
			parts[index] = strings.Join(fields, " ")
		}
	}
	return strings.Join(parts, ", ")
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
	return `(function(){
"use strict";
const targetBase=` + string(baseJSON) + `;
const targetOrigin=` + string(originJSON) + `;
const browsePrefix="/browse/";
const socketPrefix="/socket/";
const encodeOrigin=(origin)=>{const bytes=new TextEncoder().encode(origin);let raw="";for(const byte of bytes)raw+=String.fromCharCode(byte);return btoa(raw).replace(/\+/g,"-").replace(/\//g,"_").replace(/=+$/g,"");};
const passthrough=/^(?:data|blob|javascript|mailto|tel|about):/i;
const resolveTarget=(value)=>{const parsed=new URL(String(value),targetBase);if(parsed.origin===location.origin){if(parsed.pathname.startsWith(browsePrefix)||parsed.pathname.startsWith(socketPrefix))return parsed;return new URL(parsed.pathname+parsed.search+parsed.hash,targetOrigin);}return parsed;};
const proxify=(value)=>{if(value==null||passthrough.test(String(value)))return value;try{const parsed=resolveTarget(value);if(parsed.origin===location.origin&&(parsed.pathname.startsWith(browsePrefix)||parsed.pathname.startsWith(socketPrefix)))return parsed.href;if(parsed.protocol!=="http:"&&parsed.protocol!=="https:")return value;return location.origin+browsePrefix+encodeOrigin(parsed.origin)+parsed.pathname+parsed.search+parsed.hash;}catch{return value;}};
const nativeFetch=window.fetch;if(nativeFetch)window.fetch=function(input,init){if(input instanceof Request)return nativeFetch.call(this,new Request(proxify(input.url),input),init);return nativeFetch.call(this,proxify(input),init);};
const xhrOpen=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(method,url){const args=Array.from(arguments);args[1]=proxify(url);return xhrOpen.apply(this,args);};
if(window.EventSource){const NativeEventSource=window.EventSource;window.EventSource=function(url,config){return new NativeEventSource(proxify(url),config);};window.EventSource.prototype=NativeEventSource.prototype;}
if(window.WebSocket){const NativeWebSocket=window.WebSocket;window.WebSocket=function(value,protocols){const parsed=resolveTarget(value);const wsScheme=location.protocol==="https:"?"wss:":"ws:";const targetScheme=parsed.protocol==="https:"?"wss:":"ws:";const endpoint=wsScheme+"//"+location.host+socketPrefix+encodeOrigin(targetScheme+"//"+parsed.host)+parsed.pathname+parsed.search;return protocols===undefined?new NativeWebSocket(endpoint):new NativeWebSocket(endpoint,protocols);};window.WebSocket.prototype=NativeWebSocket.prototype;Object.setPrototypeOf(window.WebSocket,NativeWebSocket);}
const nativeOpen=window.open;window.open=function(url){const args=Array.from(arguments);if(args.length)args[0]=proxify(url);return nativeOpen.apply(this,args);};
const pushState=history.pushState;history.pushState=function(state,title,url){return pushState.call(this,state,title,url==null?url:proxify(url));};
const replaceState=history.replaceState;history.replaceState=function(state,title,url){return replaceState.call(this,state,title,url==null?url:proxify(url));};
window.__OWU__={target:targetBase,proxify};
})();`
}
