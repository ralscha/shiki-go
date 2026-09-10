// Package transformers provides Shiki's bundled highlighting transformers.
package transformers

import (
	shiki "github.com/ralscha/shiki-go"
	"maps"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type NotationOptions struct {
	MatchAlgorithm                                                    string
	ClassMap                                                          map[string][]string
	ClassLineAdd, ClassLineRemove                                     string
	ClassActiveLine, ClassActivePre, ClassActiveCode, ClassActiveWord string
}
type parsedComment struct {
	line, token             *shiki.Node
	prefix, content, suffix string
	only, jsx               bool
	additional              []*shiki.Node
}

var commentMatchers = []struct {
	re   *regexp.Regexp
	last bool
}{
	{regexp.MustCompile(`^(<!--)(.+)(-->)$`), false},
	{regexp.MustCompile(`^(/\*)(.+)(\*/)$`), false},
	{regexp.MustCompile(`^(//|["'#]|;{1,2}|%{1,2}|--)(.*)$`), true},
	{regexp.MustCompile(`^(\*)(.+)$`), true},
}

func matchComment(text string, last bool) ([]string, bool) {
	left := strings.TrimLeftFunc(text, unicode.IsSpace)
	front := len(text) - len(left)
	trimmed := strings.TrimRightFunc(left, unicode.IsSpace)
	back := len(left) - len(trimmed)
	for _, matcher := range commentMatchers {
		if matcher.last && !last {
			continue
		}
		m := matcher.re.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		suffix := ""
		if len(m) > 3 && m[3] != "" {
			suffix = m[3] + strings.Repeat(" ", back)
		}
		return []string{strings.Repeat(" ", front) + m[1], m[2], suffix}, true
	}
	return nil, false
}
func cloneNode(n *shiki.Node, text string) *shiki.Node {
	copy := *n
	copy.Properties = map[string]any{}
	maps.Copy(copy.Properties, n.Properties)
	copy.Children = []*shiki.Node{shiki.Text(text)}
	return &copy
}
func textHead(n *shiki.Node) (string, bool) {
	if n == nil || n.Type != "element" || len(n.Children) == 0 || n.Children[0].Type != "text" {
		return "", false
	}
	return n.Children[0].Value, true
}

var nestedComment = regexp.MustCompile(`\s+//`)

func parseComments(lines []*shiki.Node, jsx bool, algorithm string) []*parsedComment {
	var out []*parsedComment
	for _, line := range lines {
		if algorithm != "v1" {
			var children []*shiki.Node
			for i, n := range line.Children {
				text, ok := textHead(n)
				if !ok {
					children = append(children, n)
					continue
				}
				if _, ok := matchComment(text, i == len(line.Children)-1); !ok {
					children = append(children, n)
					continue
				}
				matches := nestedComment.FindAllStringIndex(text, -1)
				if len(matches) == 0 {
					children = append(children, n)
					continue
				}
				var parts []string
				last := 0
				for _, m := range matches {
					if m[0] > last {
						parts = append(parts, text[last:m[0]])
					}
					last = m[0]
				}
				if last < len(text) {
					parts = append(parts, text[last:])
				}
				if len(parts) <= 1 {
					children = append(children, n)
				} else {
					for _, part := range parts {
						children = append(children, cloneNode(n, part))
					}
				}
			}
			line.Children = children
		}
		elements := line.Children
		start := len(elements) - 1
		if algorithm == "v1" {
			start = 0
		} else if jsx {
			start = len(elements) - 2
		}
		for i := max(0, start); i < len(elements); i++ {
			token := elements[i]
			text, ok := textHead(token)
			if !ok {
				continue
			}
			last := i == len(elements)-1
			info, matched := matchComment(text, last)
			if !matched && i > 0 && strings.HasPrefix(strings.TrimSpace(text), "[!code") {
				prev := elements[i-1]
				previous, ok := textHead(prev)
				if ok && strings.Contains(previous, "//") {
					combined, ok := matchComment(previous+text, last)
					if ok {
						out = append(out, &parsedComment{line: line, token: prev, prefix: combined[0], content: combined[1], suffix: combined[2], only: len(elements) == 2 && len(prev.Children) == 1 && len(token.Children) == 1, additional: []*shiki.Node{token}})
						continue
					}
				}
			}
			if !matched {
				continue
			}
			c := &parsedComment{line: line, token: token, prefix: info[0], content: info[1], suffix: info[2]}
			if jsx && !last && i != 0 {
				c.jsx = strings.TrimSpace(shiki.TextContent(elements[i-1])) == "{" && strings.TrimSpace(shiki.TextContent(elements[i+1])) == "}"
				c.only = len(elements) == 3 && len(token.Children) == 1
			} else {
				c.only = len(elements) == 1 && len(token.Children) == 1
			}
			out = append(out, c)
		}
	}
	return out
}
func indexNode(nodes []*shiki.Node, n *shiki.Node) int {
	for i, x := range nodes {
		if x == n {
			return i
		}
	}
	return -1
}
func removeNode(nodes []*shiki.Node, n *shiki.Node) []*shiki.Node {
	i := indexNode(nodes, n)
	if i < 0 {
		return nodes
	}
	return append(nodes[:i], nodes[i+1:]...)
}

var clearEndV1 = regexp.MustCompile(`(?://|["'#]|;{1,2}|%{1,2}|--)\s*$`)
var clearEndV3 = regexp.MustCompile(`(?://|#|;{1,2}|%{1,2}|--)\s*$`)

type CommentMatchFunc func(*shiki.TransformerContext, []string, *shiki.Node, *shiki.Node, []*shiki.Node, int) bool

func CreateCommentNotationTransformer(name string, re *regexp.Regexp, onMatch CommentMatchFunc, algorithm string) shiki.Transformer {
	if algorithm == "" {
		algorithm = "v3"
	}
	return shiki.Transformer{Name: name, Code: func(ctx *shiki.TransformerContext, code *shiki.Node) (*shiki.Node, error) {
		var lines []*shiki.Node
		for _, n := range code.Children {
			if n.Type == "element" {
				lines = append(lines, n)
			}
		}
		parsed, ok := ctx.Meta["notation:comments"].([]*parsedComment)
		if !ok {
			parsed = parseComments(lines, ctx.Options.Lang == "jsx" || ctx.Options.Lang == "tsx", algorithm)
			ctx.Meta["notation:comments"] = parsed
		}
		var remove []*shiki.Node
		for _, c := range parsed {
			if c.content == "" {
				continue
			}
			index := indexNode(lines, c.line)
			if c.only && algorithm != "v1" {
				index++
			}
			replaced := false
			c.content = re.ReplaceAllStringFunc(c.content, func(s string) string {
				if onMatch(ctx, re.FindStringSubmatch(s), c.line, c.token, lines, index) {
					replaced = true
					return ""
				}
				return s
			})
			if !replaced {
				continue
			}
			if algorithm == "v1" {
				c.content = clearEndV1.ReplaceAllString(c.content, "")
			} else {
				if clearEndV3.MatchString(c.content) {
					c.content = strings.TrimRightFunc(clearEndV3.ReplaceAllString(c.content, ""), unicode.IsSpace)
				}
			}
			empty := strings.TrimSpace(c.content) == ""
			if empty {
				c.content = ""
			}
			if empty && c.only {
				remove = append(remove, c.line)
			} else if empty && c.jsx {
				idx := indexNode(c.line.Children, c.token)
				if idx > 0 && idx+1 < len(c.line.Children) {
					c.line.Children = append(c.line.Children[:idx-1], c.line.Children[idx+2:]...)
				}
			} else if empty {
				for _, n := range c.additional {
					c.line.Children = removeNode(c.line.Children, n)
				}
				c.line.Children = removeNode(c.line.Children, c.token)
			} else {
				if len(c.token.Children) > 0 && c.token.Children[0].Type == "text" {
					c.token.Children[0].Value = c.prefix + c.content + c.suffix
					for _, n := range c.additional {
						if len(n.Children) > 0 && n.Children[0].Type == "text" {
							n.Children[0].Value = ""
						}
					}
				}
			}
		}
		for _, n := range remove {
			i := indexNode(code.Children, n)
			if i < 0 {
				continue
			}
			end := i + 1
			if end < len(code.Children) && code.Children[end].Type == "text" && code.Children[end].Value == "\n" {
				end++
			}
			code.Children = append(code.Children[:i], code.Children[end:]...)
		}
		return nil, nil
	}}
}
func NotationMap(o NotationOptions) shiki.Transformer {
	keys := []string{}
	for k := range o.ClassMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		keys[i] = regexp.QuoteMeta(k)
	}
	re := regexp.MustCompile(`(?i)#?\s*\[!code (` + strings.Join(keys, "|") + `)(:\d+)?\]`)
	return CreateCommentNotationTransformer("@shikijs/transformers:notation-map", re, func(ctx *shiki.TransformerContext, m []string, _ *shiki.Node, _ *shiki.Node, lines []*shiki.Node, index int) bool {
		count := 1
		if m[2] != "" {
			count, _ = strconv.Atoi(m[2][1:])
		}
		for i := max(0, index); i < min(index+count, len(lines)); i++ {
			shiki.AddClassToHAST(lines[i], o.ClassMap[m[1]]...)
		}
		if o.ClassActivePre != "" {
			shiki.AddClassToHAST(ctx.Pre, o.ClassActivePre)
		}
		if o.ClassActiveCode != "" {
			shiki.AddClassToHAST(ctx.Code, o.ClassActiveCode)
		}
		return true
	}, o.MatchAlgorithm)
}
func notationOptions(options []NotationOptions) NotationOptions {
	if len(options) > 0 {
		return options[0]
	}
	return NotationOptions{}
}
func NotationDiff(options ...NotationOptions) shiki.Transformer {
	o := notationOptions(options)
	if o.ClassLineAdd == "" {
		o.ClassLineAdd = "diff add"
	}
	if o.ClassLineRemove == "" {
		o.ClassLineRemove = "diff remove"
	}
	if o.ClassActivePre == "" {
		o.ClassActivePre = "has-diff"
	}
	o.ClassMap = map[string][]string{"++": strings.Fields(o.ClassLineAdd), "--": strings.Fields(o.ClassLineRemove)}
	t := NotationMap(o)
	t.Name = "@shikijs/transformers:notation-diff"
	return t
}
func NotationHighlight(options ...NotationOptions) shiki.Transformer {
	o := notationOptions(options)
	if o.ClassActiveLine == "" {
		o.ClassActiveLine = "highlighted"
	}
	if o.ClassActivePre == "" {
		o.ClassActivePre = "has-highlighted"
	}
	o.ClassMap = map[string][]string{"highlight": strings.Fields(o.ClassActiveLine), "hl": strings.Fields(o.ClassActiveLine)}
	t := NotationMap(o)
	t.Name = "@shikijs/transformers:notation-highlight"
	return t
}
func NotationFocus(options ...NotationOptions) shiki.Transformer {
	o := notationOptions(options)
	if o.ClassActiveLine == "" {
		o.ClassActiveLine = "focused"
	}
	if o.ClassActivePre == "" {
		o.ClassActivePre = "has-focused"
	}
	o.ClassMap = map[string][]string{"focus": strings.Fields(o.ClassActiveLine)}
	t := NotationMap(o)
	t.Name = "@shikijs/transformers:notation-focus"
	return t
}
func NotationErrorLevel(options ...NotationOptions) shiki.Transformer {
	o := notationOptions(options)
	if o.ClassActivePre == "" {
		o.ClassActivePre = "has-highlighted"
	}
	if o.ClassMap == nil {
		o.ClassMap = map[string][]string{"error": {"highlighted", "error"}, "warning": {"highlighted", "warning"}, "info": {"highlighted", "info"}}
	}
	t := NotationMap(o)
	t.Name = "@shikijs/transformers:notation-error-level"
	return t
}
