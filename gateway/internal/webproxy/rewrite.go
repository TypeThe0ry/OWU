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
const encodeOrigin=(origin)=>{const bytes=new TextEncoder().encode(origin);let raw="";for(const byte of bytes)raw+=String.fromCharCode(byte);return btoa(raw).replace(/\+/g,"-").replace(/\//g,"_").replace(/=+$/g,"");};
const decodeOrigin=(token)=>{let raw=String(token).replace(/-/g,"+").replace(/_/g,"/");while(raw.length%4)raw+="=";const binary=atob(raw);const bytes=Uint8Array.from(binary,char=>char.charCodeAt(0));return new TextDecoder().decode(bytes);};
const passthrough=/^(?:data|blob|javascript|mailto|tel|about):/i;
const targetFromProxyURL=(value)=>{try{const parsed=new URL(value,location.origin);if(parsed.origin!==location.origin||!parsed.pathname.startsWith(browsePrefix))return null;const remainder=parsed.pathname.slice(browsePrefix.length);const slash=remainder.indexOf("/");const token=slash<0?remainder:remainder.slice(0,slash);const path=slash<0?"/":remainder.slice(slash);const origin=decodeOrigin(token);return new URL(path+parsed.search+parsed.hash,origin);}catch{return null;}};
const liveTargetBase=()=>targetFromProxyURL(document.baseURI)||targetFromProxyURL(location.href)||new URL(targetBase);
const resolveTarget=(value)=>{const text=String(value);if(text.startsWith(browsePrefix)||text.startsWith(socketPrefix))return new URL(text,location.origin);const parsed=new URL(text,liveTargetBase());if(parsed.origin===location.origin){if(parsed.pathname.startsWith(browsePrefix)||parsed.pathname.startsWith(socketPrefix))return parsed;return new URL(parsed.pathname+parsed.search+parsed.hash,targetOrigin);}return parsed;};
const proxify=(value)=>{if(value==null||passthrough.test(String(value)))return value;try{const parsed=resolveTarget(value);if(parsed.origin===location.origin&&(parsed.pathname.startsWith(browsePrefix)||parsed.pathname.startsWith(socketPrefix)))return parsed.href;if(parsed.protocol!=="http:"&&parsed.protocol!=="https:")return value;return location.origin+browsePrefix+encodeOrigin(parsed.origin)+parsed.pathname+parsed.search+parsed.hash;}catch{return value;}};
const cookieOwner=[Document.prototype,typeof HTMLDocument==="function"?HTMLDocument.prototype:null].find(prototype=>prototype&&Object.getOwnPropertyDescriptor(prototype,"cookie"));
if(cookieOwner){const nativeCookie=Object.getOwnPropertyDescriptor(cookieOwner,"cookie");try{Object.defineProperty(cookieOwner,"cookie",{configurable:nativeCookie.configurable,enumerable:nativeCookie.enumerable,get(){const stored=nativeCookie.get.call(this);return String(stored||"").split(/;\s*/).filter(Boolean).flatMap(entry=>{const separator=entry.indexOf("=");const name=(separator<0?entry:entry.slice(0,separator)).trim();return name.startsWith(cookiePrefix)?[name.slice(cookiePrefix.length)+(separator<0?"":entry.slice(separator))]:[];}).join("; ");},set(value){const segments=String(value).split(";");const pair=(segments.shift()||"").trim();const separator=pair.indexOf("=");if(separator<=0)return;let name=pair.slice(0,separator).trim();if(!name.startsWith(cookiePrefix))name=cookiePrefix+name;const attributes=segments.map(segment=>segment.trim()).filter(Boolean).filter(segment=>{const key=segment.split("=",1)[0].trim().toLowerCase();return key!=="domain"&&key!=="path";});attributes.push("Path=/");nativeCookie.set.call(this,name+pair.slice(separator)+(attributes.length?"; "+attributes.join("; "):""));}});}catch{}}
const nativeFetch=window.fetch;if(nativeFetch)window.fetch=function(input,init){if(input instanceof Request)return nativeFetch.call(this,new Request(proxify(input.url),input),init);return nativeFetch.call(this,proxify(input),init);};
const xhrOpen=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(method,url){const args=Array.from(arguments);args[1]=proxify(url);return xhrOpen.apply(this,args);};
if(window.EventSource){const NativeEventSource=window.EventSource;window.EventSource=function(url,config){return new NativeEventSource(proxify(url),config);};window.EventSource.prototype=NativeEventSource.prototype;}
if(window.WebSocket){const NativeWebSocket=window.WebSocket;window.WebSocket=function(value,protocols){const parsed=resolveTarget(value);const wsScheme=location.protocol==="https:"?"wss:":"ws:";const targetScheme=parsed.protocol==="wss:"||parsed.protocol==="https:"?"wss:":"ws:";const endpoint=wsScheme+"//"+location.host+socketPrefix+encodeOrigin(targetScheme+"//"+parsed.host)+parsed.pathname+parsed.search;return protocols===undefined?new NativeWebSocket(endpoint):new NativeWebSocket(endpoint,protocols);};window.WebSocket.prototype=NativeWebSocket.prototype;Object.setPrototypeOf(window.WebSocket,NativeWebSocket);}
const nativeOpen=window.open;window.open=function(url){const args=Array.from(arguments);if(args.length)args[0]=proxify(url);return nativeOpen.apply(this,args);};
const pushState=history.pushState;history.pushState=function(state,title,url){return pushState.call(this,state,title,url==null?url:proxify(url));};
const replaceState=history.replaceState;history.replaceState=function(state,title,url){return replaceState.call(this,state,title,url==null?url:proxify(url));};
if("NavigationEvent" in window&&window.navigation&&window.navigation.addEventListener){window.navigation.addEventListener("navigate",event=>{if(!event.canIntercept||event.hashChange||event.downloadRequest!==null)return;try{const rewritten=proxify(event.destination.url);if(rewritten!==event.destination.url)event.intercept({handler:()=>location.replace(rewritten)});}catch{}});}
if(navigator.sendBeacon){const nativeBeacon=navigator.sendBeacon.bind(navigator);navigator.sendBeacon=function(url,data){return nativeBeacon(proxify(url),data);};}
if(window.Worker){const NativeWorker=window.Worker;window.Worker=function(url,options){return new NativeWorker(proxify(url),options);};window.Worker.prototype=NativeWorker.prototype;Object.setPrototypeOf(window.Worker,NativeWorker);}
if(window.SharedWorker){const NativeSharedWorker=window.SharedWorker;window.SharedWorker=function(url,options){return new NativeSharedWorker(proxify(url),options);};window.SharedWorker.prototype=NativeSharedWorker.prototype;Object.setPrototypeOf(window.SharedWorker,NativeSharedWorker);}
if(navigator.serviceWorker){const serviceWorkerMessage="OWU disables Service Worker registration because proxied sites share one browser origin.";const rejectServiceWorker=()=>Promise.reject(new DOMException(serviceWorkerMessage,"SecurityError"));const serviceWorkerPrototype=Object.getPrototypeOf(navigator.serviceWorker);try{Object.defineProperty(serviceWorkerPrototype,"register",{configurable:true,value:rejectServiceWorker});}catch{}try{Object.defineProperty(navigator.serviceWorker,"register",{configurable:true,value:rejectServiceWorker});}catch{try{navigator.serviceWorker.register=rejectServiceWorker;}catch{}}if(navigator.serviceWorker.getRegistrations)navigator.serviceWorker.getRegistrations().then(registrations=>Promise.all(registrations.map(registration=>registration.unregister()))).catch(()=>{});}
const urlAttributes=new Set(["href","src","action","formaction","poster","data","cite","background","xlink:href","data-src","data-href","data-url","data-original","data-lazy-src","data-background"]);
const proxifySrcset=(value)=>String(value).split(",").map(part=>{const fields=part.trim().split(/\s+/);if(fields[0])fields[0]=proxify(fields[0]);return fields.join(" ");}).join(", ");
const proxifyStyle=(value)=>String(value).replace(/url\(\s*(["']?)(.*?)\1\s*\)/gi,(match,quote,raw)=>"url("+quote+proxify(raw)+quote+")");
const rewriteAttributeValue=(name,value)=>{const lower=String(name).toLowerCase();if(urlAttributes.has(lower))return proxify(value);if(lower==="srcset"||lower==="imagesrcset"||lower==="data-srcset")return proxifySrcset(value);if(lower==="ping")return String(value).trim().split(/\s+/).map(proxify).join(" ");if(lower==="style")return proxifyStyle(value);return value;};
const nativeSetAttribute=Element.prototype.setAttribute;Element.prototype.setAttribute=function(name,value){return nativeSetAttribute.call(this,name,rewriteAttributeValue(name,value));};
const patchURLProperty=(prototype,name,transform=proxify)=>{try{const descriptor=Object.getOwnPropertyDescriptor(prototype,name);if(!descriptor||!descriptor.set||!descriptor.get)return;Object.defineProperty(prototype,name,{configurable:descriptor.configurable,enumerable:descriptor.enumerable,get:descriptor.get,set(value){descriptor.set.call(this,transform(value));}});}catch{}};
for(const [prototype,name,transform] of [[HTMLAnchorElement.prototype,"href"],[HTMLAreaElement.prototype,"href"],[HTMLImageElement.prototype,"src"],[HTMLImageElement.prototype,"srcset",proxifySrcset],[HTMLScriptElement.prototype,"src"],[HTMLLinkElement.prototype,"href"],[HTMLFormElement.prototype,"action"],[HTMLIFrameElement.prototype,"src"],[HTMLSourceElement.prototype,"src"],[HTMLSourceElement.prototype,"srcset",proxifySrcset],[HTMLMediaElement.prototype,"src"]])patchURLProperty(prototype,name,transform);
if(window.CSSStyleSheet&&CSSStyleSheet.prototype.insertRule){const nativeInsertRule=CSSStyleSheet.prototype.insertRule;CSSStyleSheet.prototype.insertRule=function(rule,index){return nativeInsertRule.call(this,proxifyStyle(rule),index);};}
const rewriteElement=(element)=>{if(!(element instanceof Element))return;for(const attribute of Array.from(element.attributes||[])){const rewritten=rewriteAttributeValue(attribute.name,attribute.value);if(rewritten!==attribute.value)nativeSetAttribute.call(element,attribute.name,rewritten);}};
const rewriteTree=(node)=>{rewriteElement(node);if(node&&node.querySelectorAll)for(const child of node.querySelectorAll("[href],[src],[action],[formaction],[poster],[data],[cite],[background],[xlink\\:href],[srcset],[imagesrcset],[data-src],[data-href],[data-url],[data-original],[data-lazy-src],[data-background],[data-srcset],[ping],[style]"))rewriteElement(child);};
document.addEventListener("click",event=>{const anchor=event.target&&event.target.closest?event.target.closest("a[href]"):null;if(anchor)nativeSetAttribute.call(anchor,"href",proxify(anchor.getAttribute("href")));},true);
document.addEventListener("submit",event=>{const form=event.target;if(form instanceof HTMLFormElement&&form.hasAttribute("action"))nativeSetAttribute.call(form,"action",proxify(form.getAttribute("action")));},true);
const observer=new MutationObserver(records=>{for(const record of records){if(record.type==="attributes")rewriteElement(record.target);for(const node of record.addedNodes)rewriteTree(node);}});observer.observe(document.documentElement,{subtree:true,childList:true,attributes:true,attributeFilter:["href","src","action","formaction","poster","data","cite","background","xlink:href","srcset","imagesrcset","data-src","data-href","data-url","data-original","data-lazy-src","data-background","data-srcset","ping","style"]});
rewriteTree(document.documentElement);
window.__OWU__={target:targetBase,proxify,rewrite:rewriteTree};
})();`
}
