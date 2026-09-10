package shiki

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// HASTOptions configures HTML serialization. The zero value uses the same
// defaults as Shiki's hastToHtml (hast-util-to-html).
type HASTOptions struct {
	Space                    string                    `json:"space,omitempty"`
	Quote                    string                    `json:"quote,omitempty"`
	QuoteSmart               bool                      `json:"quoteSmart,omitempty"`
	PreferUnquoted           bool                      `json:"preferUnquoted,omitempty"`
	OmitOptionalTags         bool                      `json:"omitOptionalTags,omitempty"`
	AllowParseErrors         bool                      `json:"allowParseErrors,omitempty"`
	AllowDangerousCharacters bool                      `json:"allowDangerousCharacters,omitempty"`
	AllowDangerousHTML       bool                      `json:"allowDangerousHtml,omitempty"`
	TightAttributes          bool                      `json:"tightAttributes,omitempty"`
	UpperDoctype             bool                      `json:"upperDoctype,omitempty"`
	TightDoctype             bool                      `json:"tightDoctype,omitempty"`
	BogusComments            bool                      `json:"bogusComments,omitempty"`
	TightCommaSeparatedLists bool                      `json:"tightCommaSeparatedLists,omitempty"`
	TightSelfClosing         bool                      `json:"tightSelfClosing,omitempty"`
	CollapseEmptyAttributes  bool                      `json:"collapseEmptyAttributes,omitempty"`
	CloseSelfClosing         bool                      `json:"closeSelfClosing,omitempty"`
	CloseEmptyElements       bool                      `json:"closeEmptyElements,omitempty"`
	Voids                    []string                  `json:"voids,omitempty"`
	CharacterReferences      CharacterReferenceOptions `json:"characterReferences"`
}
type CharacterReferenceOptions struct {
	UseNamedReferences     bool `json:"useNamedReferences,omitempty"`
	UseShortestReferences  bool `json:"useShortestReferences,omitempty"`
	OmitOptionalSemicolons bool `json:"omitOptionalSemicolons,omitempty"`
	Attribute              bool `json:"attribute,omitempty"`
}

func encodeHAST(value, subset string, attribute bool, options CharacterReferenceOptions) string {
	var out strings.Builder
	for i, c := range value {
		if !strings.ContainsRune(subset, c) {
			out.WriteRune(c)
			continue
		}
		next := byte(0)
		if i+1 < len(value) {
			next = value[i+1]
		}
		hexDigit := next >= '0' && next <= '9' || next >= 'a' && next <= 'f' || next >= 'A' && next <= 'F'
		numeric := "&#x" + strings.ToUpper(strconv.FormatInt(int64(c), 16))
		if !options.OmitOptionalSemicolons || next == 0 || hexDigit {
			numeric += ";"
		}
		named := ""
		if options.UseNamedReferences || options.UseShortestReferences {
			name := map[rune]string{'&': "amp", '<': "lt", '>': "gt", '"': "quot"}[c]
			if name != "" {
				named = "&" + name
				alphanumeric := next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z' || next >= '0' && next <= '9'
				if !options.OmitOptionalSemicolons || name == "lt" || name == "gt" || (attribute || options.Attribute) && (next == 0 || next == '=' || alphanumeric) {
					named += ";"
				}
			}
		}
		if options.UseShortestReferences {
			decimal := "&#" + strconv.FormatInt(int64(c), 10)
			if !options.OmitOptionalSemicolons || next == 0 || next >= '0' && next <= '9' {
				decimal += ";"
			}
			if len(decimal) < len(numeric) {
				numeric = decimal
			}
		}
		if named != "" && (!options.UseShortestReferences || len(named) < len(numeric)) {
			out.WriteString(named)
		} else {
			out.WriteString(numeric)
		}
	}
	return out.String()
}

func hastTruthy(value any) bool {
	if value == nil {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0 && !math.IsNaN(v)
	case float32:
		return v != 0 && !math.IsNaN(float64(v))
	}
	r := reflect.ValueOf(value)
	switch r.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return r.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return r.Uint() != 0
	}
	return true
}
func hastString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case map[string]any:
		return "[object Object]"
	}
	return fmt.Sprint(value)
}
func hastAttribute(key string, value any, svg bool, o HASTOptions) string {
	info := findAttribute(key, svg)
	if s, ok := value.(string); ok {
		if info.OverloadedBoolean && (s == info.Attribute || s == "") {
			value = true
		} else if info.Boolean && (s == info.Attribute || s == "") {
			value = s != ""
		}
	} else if info.Boolean || info.OverloadedBoolean {
		value = hastTruthy(value)
	}
	if value == nil || value == false {
		return ""
	}
	if n, ok := value.(float64); ok && math.IsNaN(n) {
		return ""
	}
	nameSubset := "\x00\t\n\f\r \"&'/<=>`"
	if o.AllowParseErrors && !svg {
		nameSubset = "\t\n\f\r \"&'/=>`"
	}
	if o.AllowDangerousCharacters {
		nameSubset = strings.ReplaceAll(nameSubset, "`", "")
		if o.AllowParseErrors && !svg {
			nameSubset = "\t\n\f\r &/=>"
		}
	}
	name := encodeHAST(info.Attribute, nameSubset, o.CharacterReferences.Attribute, o.CharacterReferences)
	if value == true {
		return name
	}
	s := hastString(value)
	r := reflect.ValueOf(value)
	if r.Kind() == reflect.Slice || r.Kind() == reflect.Array {
		parts := make([]string, r.Len())
		for i := range parts {
			parts[i] = hastString(r.Index(i).Interface())
		}
		sep := " "
		if info.CommaSeparated {
			sep = ", "
			if o.TightCommaSeparatedLists {
				sep = ","
			}
		}
		s = strings.Join(parts, sep)
	}
	if o.CollapseEmptyAttributes && s == "" {
		return name
	}
	if o.PreferUnquoted {
		subset := "\x00\t\n\f\r \"&'<=>`"
		if o.AllowParseErrors && !svg && o.AllowDangerousCharacters {
			subset = "\t\n\f\r &>"
		}
		encoded := encodeHAST(s, subset, true, o.CharacterReferences)
		if encoded == s {
			if s == "" {
				return name
			}
			return name + "=" + s
		}
	}
	quote := o.Quote
	if quote == "" {
		quote = "\""
	}
	other := "'"
	if quote == "'" {
		other = "\""
	}
	if o.QuoteSmart && strings.Count(s, quote) > strings.Count(s, other) {
		quote = other
	}
	subset := "\x00\"&'`"
	if o.AllowParseErrors && !svg {
		subset = "\"&'`"
	}
	if o.AllowDangerousCharacters {
		subset = "\x00&" + quote
		if o.AllowParseErrors && !svg {
			subset = "&" + quote
		}
	}
	return name + "=" + quote + encodeHAST(s, subset, true, o.CharacterReferences) + quote
}

// HASTToHTML serializes a HAST tree, with optional formatting controls. Invalid
// node types or quotes panic, like the corresponding JavaScript utility throws.
// SerializeHAST provides the same operation with an explicit error result.
func HASTToHTML(root *Node, options ...HASTOptions) string {
	var o HASTOptions
	if len(options) > 0 {
		o = options[0]
	}
	html, err := SerializeHAST(root, o)
	if err != nil {
		panic(err)
	}
	return html
}
func SerializeHAST(root *Node, o HASTOptions) (string, error) {
	if o.Quote != "" && o.Quote != "\"" && o.Quote != "'" {
		return "", fmt.Errorf("shiki: invalid HTML quote %q", o.Quote)
	}
	voids := voidElements
	if o.Voids != nil {
		voids = map[string]bool{}
		for _, tag := range o.Voids {
			voids[tag] = true
		}
	}
	var visit func(*Node, *Node, int, bool) (string, error)
	all := func(n *Node, svg bool) (string, error) {
		var b strings.Builder
		if n != nil {
			for i, c := range n.Children {
				s, err := visit(c, n, i, svg)
				if err != nil {
					return "", err
				}
				b.WriteString(s)
			}
		}
		return b.String(), nil
	}
	visit = func(n, parent *Node, index int, svg bool) (string, error) {
		if n == nil {
			return "", nil
		}
		switch n.Type {
		case "root":
			return all(n, svg)
		case "text", "raw":
			if n.Type == "raw" && o.AllowDangerousHTML || parent != nil && parent.Type == "element" && (parent.TagName == "script" || parent.TagName == "style") {
				return n.Value, nil
			}
			return encodeHAST(n.Value, "<&", false, o.CharacterReferences), nil
		case "doctype":
			word := "doctype"
			if o.UpperDoctype {
				word = "DOCTYPE"
			}
			space := " "
			if o.TightDoctype {
				space = ""
			}
			return "<!" + word + space + "html>", nil
		case "comment":
			if o.BogusComments {
				return "<?" + encodeHAST(n.Value, ">", false, o.CharacterReferences) + ">", nil
			}
			return "<!--" + commentPattern.ReplaceAllStringFunc(n.Value, func(s string) string { return encodeHAST(s, "<>", false, o.CharacterReferences) }) + "-->", nil
		case "element":
		default:
			return "", fmt.Errorf("shiki: cannot serialize HAST node type %q", n.Type)
		}
		wasSVG := svg
		if n.TagName == "svg" {
			svg = true
		}
		keys := []string{}
		for _, key := range n.propertyOrder {
			if _, ok := n.Properties[key]; ok && !hasString(keys, key) {
				keys = append(keys, key)
			}
		}
		var rest []string
		for key := range n.Properties {
			if !hasString(keys, key) {
				rest = append(rest, key)
			}
		}
		sort.Strings(rest)
		keys = append(keys, rest...)
		attributes := ""
		for _, key := range keys {
			attr := hastAttribute(key, n.Properties[key], svg, o)
			if attr != "" {
				if attributes != "" && (!o.TightAttributes || !strings.HasSuffix(attributes, "\"") && !strings.HasSuffix(attributes, "'")) {
					attributes += " "
				}
				attributes += attr
			}
		}
		contentNode := n
		if !wasSVG && n.TagName == "template" {
			contentNode = n.Content
		}
		content, err := all(contentNode, svg)
		if err != nil {
			return "", err
		}
		selfClosing := voids[strings.ToLower(n.TagName)]
		if wasSVG {
			selfClosing = o.CloseEmptyElements
		}
		if content != "" {
			selfClosing = false
		}
		omit := !wasSVG && o.OmitOptionalTags
		var b strings.Builder
		if attributes != "" || !omit || !omitOpening(n, parent, index) {
			b.WriteString("<" + n.TagName)
			if attributes != "" {
				b.WriteByte(' ')
				b.WriteString(attributes)
			}
			if selfClosing && (wasSVG || o.CloseSelfClosing) {
				last := byte(0)
				if len(attributes) > 0 {
					last = attributes[len(attributes)-1]
				}
				if !o.TightSelfClosing || last == '/' || last != 0 && last != 34 && last != 39 {
					b.WriteByte(' ')
				}
				b.WriteByte('/')
			}
			b.WriteByte('>')
		}
		b.WriteString(content)
		if !selfClosing && (!omit || !omitClosing(n, parent, index)) {
			b.WriteString("</" + n.TagName + ">")
		}
		return b.String(), nil
	}
	return visit(root, nil, 0, o.Space == "svg")
}

func hastWhitespace(n *Node) bool {
	return n != nil && n.Type == "text" && strings.Trim(n.Value, " \t\n\r\f") == ""
}
func hastFirstWhitespace(n *Node) bool {
	return n != nil && n.Type == "text" && len(n.Value) > 0 && strings.ContainsRune(" \t\n\r\f", rune(n.Value[0]))
}
func hastSibling(parent *Node, index, direction int, includeWhitespace bool) (*Node, int) {
	if parent == nil {
		return nil, -1
	}
	for i := index + direction; i >= 0 && i < len(parent.Children); i += direction {
		n := parent.Children[i]
		if includeWhitespace || !hastWhitespace(n) {
			return n, i
		}
	}
	return nil, -1
}
func hastTag(n *Node, names ...string) bool {
	return n != nil && n.Type == "element" && hasString(names, n.TagName)
}
func omitOpening(n, parent *Node, index int) bool {
	head, _ := hastSibling(n, -1, 1, false)
	switch n.TagName {
	case "html":
		return head == nil || head.Type != "comment"
	case "head":
		seen := map[string]bool{}
		for _, child := range n.Children {
			if hastTag(child, "base", "title") {
				if seen[child.TagName] {
					return false
				}
				seen[child.TagName] = true
			}
		}
		return len(n.Children) == 0 || n.Children[0].Type == "element"
	case "body":
		head, _ = hastSibling(n, -1, 1, true)
		return head == nil || head.Type != "comment" && !hastFirstWhitespace(head) && !hastTag(head, "meta", "link", "script", "style", "template")
	case "colgroup":
		previous, p := hastSibling(parent, index, -1, false)
		if hastTag(previous, "colgroup") && omitClosing(previous, parent, p) {
			return false
		}
		head, _ = hastSibling(n, -1, 1, true)
		return hastTag(head, "col")
	case "tbody":
		previous, p := hastSibling(parent, index, -1, false)
		if hastTag(previous, "thead", "tbody") && omitClosing(previous, parent, p) {
			return false
		}
		return hastTag(head, "tr")
	}
	return false
}
func omitClosing(n, parent *Node, index int) bool {
	next, _ := hastSibling(parent, index, 1, false)
	switch n.TagName {
	case "head", "colgroup", "caption":
		next, _ = hastSibling(parent, index, 1, true)
		return next == nil || next.Type != "comment" && !hastFirstWhitespace(next)
	case "html", "body":
		return next == nil || next.Type != "comment"
	case "p":
		if next != nil {
			return hastTag(next, "address", "article", "aside", "blockquote", "details", "div", "dl", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hgroup", "hr", "main", "menu", "nav", "ol", "p", "pre", "section", "table", "ul")
		}
		return !hastTag(parent, "a", "audio", "del", "ins", "map", "noscript", "video")
	case "li":
		return next == nil || hastTag(next, "li")
	case "dt":
		return hastTag(next, "dt", "dd")
	case "dd":
		return next == nil || hastTag(next, "dt", "dd")
	case "rp", "rt":
		return next == nil || hastTag(next, "rp", "rt")
	case "optgroup":
		return next == nil || hastTag(next, "optgroup")
	case "option":
		return next == nil || hastTag(next, "option", "optgroup")
	case "thead":
		return hastTag(next, "tbody", "tfoot")
	case "tbody":
		return next == nil || hastTag(next, "tbody", "tfoot")
	case "tfoot":
		return next == nil
	case "tr":
		return next == nil || hastTag(next, "tr")
	case "td", "th":
		return next == nil || hastTag(next, "td", "th")
	}
	return false
}
