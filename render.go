package shiki

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

func Element(tag string, properties map[string]any, children ...*Node) *Node {
	if properties == nil {
		properties = map[string]any{}
	}
	if children == nil {
		children = []*Node{}
	}
	return &Node{Type: "element", TagName: tag, Properties: properties, Children: children}
}
func Text(value string) *Node { return &Node{Type: "text", Value: value} }
func (n *Node) SetProperty(name string, value any) {
	if n.Properties == nil {
		n.Properties = map[string]any{}
	}
	if _, ok := n.Properties[name]; !ok {
		n.propertyOrder = append(n.propertyOrder, name)
	}
	n.Properties[name] = value
}
func (n *Node) MarshalJSON() ([]byte, error) {
	out := map[string]any{"type": n.Type}
	if n.Type == "element" {
		out["tagName"] = n.TagName
		props := n.Properties
		if props == nil {
			props = map[string]any{}
		}
		out["properties"] = props
	}
	switch n.Type {
	case "text", "raw", "comment":
		out["value"] = n.Value
	case "root", "element":
		children := n.Children
		if children == nil {
			children = []*Node{}
		}
		out["children"] = children
	}
	if n.Data != nil {
		out["data"] = n.Data
	}
	if n.Content != nil {
		out["content"] = n.Content
	}
	return json.Marshal(out)
}

func (n *Node) UnmarshalJSON(data []byte) error {
	type alias Node
	if err := json.Unmarshal(data, (*alias)(n)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	n.propertyOrder = objectKeys(fields["properties"])
	return nil
}
func AddClassToHAST(node *Node, classes ...string) *Node {
	current := []string{}
	switch v := node.Properties["class"].(type) {
	case string:
		current = strings.Fields(v)
	case []string:
		current = append(current, v...)
	case []any:
		for _, value := range v {
			if s, ok := value.(string); ok {
				current = append(current, s)
			}
		}
	}
	for _, class := range classes {
		for name := range strings.FieldsSeq(class) {
			if !hasString(current, name) {
				current = append(current, name)
			}
		}
	}
	node.SetProperty("class", current)
	return node
}
func TextContent(n *Node) string {
	if n == nil {
		return ""
	}
	if n.Type == "text" {
		return n.Value
	}
	var b strings.Builder
	for _, child := range n.Children {
		b.WriteString(TextContent(child))
	}
	return b.String()
}
func transformers(o Options) []Transformer {
	out := append([]Transformer(nil), o.Transformers...)
	priority := func(v string) int {
		switch v {
		case "pre":
			return -1
		case "post":
			return 1
		}
		return 0
	}
	sort.SliceStable(out, func(i, j int) bool { return priority(out[i].Enforce) < priority(out[j].Enforce) })
	return append(out, TransformerDecorations())
}
func mergeWhitespace(tokens [][]Token, mode any) [][]Token {
	out := make([][]Token, len(tokens))
	for i, line := range tokens {
		out[i] = []Token{}
		if mode == false {
			out[i] = append(out[i], line...)
			continue
		}
		if mode == "never" {
			for _, t := range line {
				if strings.TrimSpace(t.Content) == "" {
					out[i] = append(out[i], t)
					continue
				}
				middle := strings.TrimFunc(t.Content, unicode.IsSpace)
				left := len(t.Content) - len(strings.TrimLeftFunc(t.Content, unicode.IsSpace))
				right := left + len(middle)
				if left > 0 {
					out[i] = append(out[i], Token{Content: t.Content[:left], Offset: t.Offset})
				}
				copy := t
				copy.Content = middle
				copy.Offset += UTF16Len(t.Content[:left])
				out[i] = append(out[i], copy)
				if right < len(t.Content) {
					out[i] = append(out[i], Token{Content: t.Content[right:], Offset: t.Offset + UTF16Len(t.Content[:right])})
				}
			}
			continue
		}
		carry := ""
		offset := 0
		for j, t := range line {
			decorated := t.FontStyle&(FontStyleUnderline|FontStyleStrikethrough) != 0
			if !decorated && t.Content != "" && strings.TrimSpace(t.Content) == "" && j+1 < len(line) {
				if carry == "" {
					offset = t.Offset
				}
				carry += t.Content
				continue
			}
			if carry != "" {
				if decorated {
					out[i] = append(out[i], Token{Content: carry, Offset: offset})
				} else {
					t.Content = carry + t.Content
					t.Offset = offset
				}
				carry = ""
			}
			out[i] = append(out[i], t)
		}
	}
	return out
}
func MergeAdjacentStyledTokens(tokens [][]Token) [][]Token {
	out := make([][]Token, len(tokens))
	for i, line := range tokens {
		out[i] = []Token{}
		for _, t := range line {
			n := len(out[i])
			if n > 0 {
				prev := &out[i][n-1]
				if (t.FontStyle|prev.FontStyle)&(FontStyleUnderline|FontStyleStrikethrough) == 0 && StringifyTokenStyle(tokenStyle(t)) == StringifyTokenStyle(tokenStyle(*prev)) {
					prev.Content += t.Content
					continue
				}
			}
			out[i] = append(out[i], t)
		}
	}
	return out
}
func (h *Highlighter) render(code string, o Options) (*Node, *TransformerContext, error) {
	ctx := &TransformerContext{Highlighter: h, Options: &o, Source: code, Meta: map[string]any{}}
	hooks := transformers(o)
	for _, tr := range hooks {
		if tr.Preprocess != nil {
			next, err := tr.Preprocess(ctx, ctx.Source)
			if err != nil {
				return nil, nil, err
			}
			if next != "" {
				ctx.Source = next
			}
		}
	}
	result, err := h.CodeToTokens(ctx.Source, o)
	if err != nil {
		return nil, nil, err
	}
	result.Tokens = mergeWhitespace(result.Tokens, o.MergeWhitespaces)
	if o.MergeSameStyleTokens {
		result.Tokens = MergeAdjacentStyledTokens(result.Tokens)
	}
	for _, tr := range hooks {
		if tr.Tokens != nil {
			next, err := tr.Tokens(ctx, result.Tokens)
			if err != nil {
				return nil, nil, err
			}
			if next != nil {
				result.Tokens = next
			}
		}
	}
	root, err := tokensToHAST(result, o, ctx)
	return root, ctx, err
}
func tokensToHAST(result *TokensResult, o Options, ctx *TransformerContext) (*Node, error) {
	hooks := transformers(o)
	tokens := result.Tokens
	root := &Node{Type: "root", Children: []*Node{}, GrammarState: result.GrammarState}
	pre := Element("pre", nil)
	pre.SetProperty("class", "shiki "+result.ThemeName)
	if o.RootStyle != false {
		style, ok := o.RootStyle.(string)
		if !ok {
			style = result.RootStyle
			if style == "" {
				style = "background-color:" + result.BG + ";color:" + result.FG
			}
		}
		pre.SetProperty("style", style)
	}
	if o.TabIndex != false {
		tab := "0"
		if o.TabIndex != nil {
			tab = fmt.Sprint(o.TabIndex)
		}
		pre.SetProperty("tabindex", tab)
	}
	metaKeys := make([]string, 0, len(o.Meta))
	for key := range o.Meta {
		if !strings.HasPrefix(key, "_") {
			metaKeys = append(metaKeys, key)
		}
	}
	sort.Strings(metaKeys)
	if len(o.metaOrder) > 0 {
		ordered := []string{}
		for _, key := range o.metaOrder {
			if hasString(metaKeys, key) {
				ordered = append(ordered, key)
			}
		}
		for _, key := range metaKeys {
			if !hasString(ordered, key) {
				ordered = append(ordered, key)
			}
		}
		metaKeys = ordered
	}
	for _, key := range metaKeys {
		pre.SetProperty(key, o.Meta[key])
	}
	pre.Data = o.Data
	code := Element("code", nil)
	ctx.Root, ctx.Pre, ctx.Code, ctx.Tokens = root, pre, code, tokens
	ctx.Lines = []*Node{}
	inline := o.Structure == "inline"
	ctx.Structure = o.Structure
	if ctx.Structure == "" {
		ctx.Structure = "classic"
	}
	if o.Structure != "" && o.Structure != "classic" && !inline {
		return nil, fmt.Errorf("shiki: unsupported structure %q", o.Structure)
	}
	for i, line := range tokens {
		if i > 0 && !inline {
			code.Children = append(code.Children, Text("\n"))
		}
		lineNode := Element("span", nil)
		lineNode.SetProperty("class", "line")
		col := 0
		for _, token := range line {
			span := Element("span", nil, Text(token.Content))
			keys := make([]string, 0, len(token.HTMLAttrs))
			for k := range token.HTMLAttrs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			ordered := append([]string(nil), token.attrOrder...)
			for _, key := range keys {
				if !hasString(ordered, key) {
					ordered = append(ordered, key)
				}
			}
			keys = ordered
			for _, k := range keys {
				span.SetProperty(k, token.HTMLAttrs[k])
			}
			if style := StringifyTokenStyle(tokenStyle(token)); style != "" {
				span.SetProperty("style", style)
			}
			for _, tr := range hooks {
				if tr.Span != nil {
					next, err := tr.Span(ctx, span, i+1, col, lineNode, token)
					if err != nil {
						return nil, err
					}
					if next != nil {
						span = next
					}
				}
			}
			lineNode.Children = append(lineNode.Children, span)
			col += UTF16Len(token.Content)
		}
		if !inline {
			for _, tr := range hooks {
				if tr.Line != nil {
					next, err := tr.Line(ctx, lineNode, i+1)
					if err != nil {
						return nil, err
					}
					if next != nil {
						lineNode = next
					}
				}
			}
		}
		ctx.Lines = append(ctx.Lines, lineNode)
		code.Children = append(code.Children, lineNode)
	}
	for _, tr := range hooks {
		if tr.Code != nil {
			next, err := tr.Code(ctx, code)
			if err != nil {
				return nil, err
			}
			if next != nil {
				code = next
			}
		}
	}
	ctx.Code = code
	if inline {
		for i, line := range code.Children {
			if i > 0 {
				root.Children = append(root.Children, Element("br", nil))
			}
			if line.Type == "element" {
				root.Children = append(root.Children, line.Children...)
			}
		}
	} else {
		pre.Children = append(pre.Children, code)
		for _, tr := range hooks {
			if tr.Pre != nil {
				next, err := tr.Pre(ctx, pre)
				if err != nil {
					return nil, err
				}
				if next != nil {
					pre = next
				}
			}
		}
		ctx.Pre = pre
		root.Children = append(root.Children, pre)
	}
	for _, tr := range hooks {
		if tr.Root != nil {
			next, err := tr.Root(ctx, root)
			if err != nil {
				return nil, err
			}
			if next != nil {
				root = next
			}
		}
	}
	root.GrammarState = result.GrammarState
	return root, nil
}
func TokensToHAST(result *TokensResult, o Options) (*Node, error) {
	return tokensToHAST(result, o, &TransformerContext{Options: &o, Meta: map[string]any{}})
}
func (h *Highlighter) CodeToHAST(code string, o Options) (*Node, error) {
	root, _, err := h.render(code, o)
	return root, err
}
func (h *Highlighter) CodeToHTML(code string, o Options) (string, error) {
	root, ctx, err := h.render(code, o)
	if err != nil {
		return "", err
	}
	html := HASTToHTML(root)
	for _, tr := range transformers(o) {
		if tr.Postprocess != nil {
			next, err := tr.Postprocess(ctx, html)
			if err != nil {
				return "", err
			}
			if next != "" {
				html = next
			}
		}
	}
	return html, nil
}
func CodeToHAST(code string, o Options) (*Node, error) {
	h, err := shorthand(code, o)
	if err != nil {
		return nil, err
	}
	return h.CodeToHAST(code, o)
}
func CodeToHTML(code string, o Options) (string, error) {
	h, err := shorthand(code, o)
	if err != nil {
		return "", err
	}
	return h.CodeToHTML(code, o)
}
func (h *Highlighter) CodeToHtml(code string, o Options) (string, error) {
	return h.CodeToHTML(code, o)
}
func (h *Highlighter) CodeToHast(code string, o Options) (*Node, error) { return h.CodeToHAST(code, o) }

var commentPattern = regexp.MustCompile(`^>|^->|<!--|-->|--!>|<!-$`)

//go:embed assets/hast-properties.json
var hastPropertiesJSON []byte

type attributeInfo struct {
	Attribute                                  string
	Boolean, OverloadedBoolean, CommaSeparated bool
}

var hastProperties = func() map[string]map[string]attributeInfo {
	var result map[string]map[string]attributeInfo
	if err := json.Unmarshal(hastPropertiesJSON, &result); err != nil {
		panic(err)
	}
	return result
}()
var dataAttribute = regexp.MustCompile(`(?i)^data[-\w.:]+$`)
var dataDash = regexp.MustCompile(`-[a-z]`)

func findAttribute(key string, svg bool) attributeInfo {
	schema := "html"
	if svg {
		schema = "svg"
	}
	if info, ok := hastProperties[schema][strings.ToLower(key)]; ok {
		return info
	}
	name := key
	if len(key) > 4 && strings.EqualFold(key[:4], "data") && dataAttribute.MatchString(key) && key[4] != '-' && !dataDash.MatchString(key[4:]) {
		var suffix strings.Builder
		for _, c := range key[4:] {
			if c >= 'A' && c <= 'Z' {
				suffix.WriteByte('-')
				suffix.WriteRune(c + 32)
			} else {
				suffix.WriteRune(c)
			}
		}
		rest := suffix.String()
		if !strings.HasPrefix(rest, "-") {
			rest = "-" + rest
		}
		name = "data" + rest
	}
	return attributeInfo{Attribute: name}
}

var voidElements = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true, "input": true, "link": true, "meta": true, "param": true, "source": true, "track": true, "wbr": true}
