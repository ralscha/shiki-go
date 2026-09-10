package transformers

import (
	"fmt"
	"regexp"
	shiki "shiki-go"
	"strconv"
	"strings"
)

type RenderWhitespaceOptions struct{ ClassTab, ClassSpace, Position string }

func isSpace(s string) bool { return s == " " || s == "\t" }
func SeparateContinuousSpaces(inputs []string) []string {
	out := []string{}
	current := ""
	flush := func() {
		if current != "" {
			out = append(out, current)
		}
		current = ""
	}
	for i, part := range inputs {
		if part == "\t" || isSpace(part) && (i > 0 && isSpace(inputs[i-1]) || i+1 < len(inputs) && isSpace(inputs[i+1])) {
			flush()
			out = append(out, part)
		} else {
			current += part
		}
	}
	flush()
	return out
}
func SplitSpaces(parts []string, position string, continuous bool) []string {
	if position == "all" {
		return parts
	}
	left, right := 0, 0
	if position == "boundary" || position == "leading" {
		for left < len(parts) && isSpace(parts[left]) {
			left++
		}
	}
	if position == "boundary" || position == "trailing" {
		for right < len(parts) && isSpace(parts[len(parts)-1-right]) {
			right++
		}
	}
	end := max(left, len(parts)-right)
	middle := parts[left:end]
	out := append([]string{}, parts[:left]...)
	if continuous {
		out = append(out, SeparateContinuousSpaces(middle)...)
	} else {
		out = append(out, strings.Join(middle, ""))
	}
	out = append(out, parts[len(parts)-right:]...)
	return out
}
func whitespaceParts(s string) []string {
	parts := []string{}
	last := 0
	for i, c := range s {
		if c == ' ' || c == '\t' {
			if i > last {
				parts = append(parts, s[last:i])
			}
			parts = append(parts, string(c))
			last = i + 1
		}
	}
	if last < len(s) {
		parts = append(parts, s[last:])
	}
	return parts
}
func RenderWhitespace(options ...RenderWhitespaceOptions) shiki.Transformer {
	var o RenderWhitespaceOptions
	if len(options) > 0 {
		o = options[0]
	}
	if o.ClassTab == "" {
		o.ClassTab = "tab"
	}
	if o.ClassSpace == "" {
		o.ClassSpace = "space"
	}
	if o.Position == "" {
		o.Position = "all"
	}
	return shiki.Transformer{Name: "@shikijs/transformers:render-whitespace", Root: func(_ *shiki.TransformerContext, root *shiki.Node) (*shiki.Node, error) {
		lines := []*shiki.Node{root}
		if len(root.Children) > 0 && root.Children[0].TagName == "pre" && len(root.Children[0].Children) > 0 {
			lines = root.Children[0].Children[0].Children
		}
		for _, line := range lines {
			if line.Type != "element" && line.Type != "root" {
				continue
			}
			var elements []*shiki.Node
			for _, n := range line.Children {
				if n.Type == "element" {
					elements = append(elements, n)
				}
			}
			last := len(elements) - 1
			var children []*shiki.Node
			for _, token := range line.Children {
				index := indexNode(elements, token)
				text, ok := textHead(token)
				if !ok || text == "" || o.Position == "boundary" && index != 0 && index != last || o.Position == "trailing" && index != last || o.Position == "leading" && index != 0 {
					children = append(children, token)
					continue
				}
				position := o.Position
				if position == "boundary" && index == last && last != 0 {
					position = "trailing"
				}
				parts := SplitSpaces(whitespaceParts(text), position, o.Position != "trailing" && o.Position != "leading")
				if len(parts) <= 1 {
					children = append(children, token)
					continue
				}
				for _, part := range parts {
					clone := cloneNode(token, part)
					switch part {
					case " ":
						shiki.AddClassToHAST(clone, o.ClassSpace)
						delete(clone.Properties, "style")
					case "\t":
						shiki.AddClassToHAST(clone, o.ClassTab)
						delete(clone.Properties, "style")
					}
					children = append(children, clone)
				}
			}
			line.Children = children
		}
		return nil, nil
	}}
}

type RenderIndentGuidesOptions struct{ Indent any }

var indentMeta = regexp.MustCompile(`\{indent:(\d+|false)\}`)

func RenderIndentGuides(options ...RenderIndentGuidesOptions) shiki.Transformer {
	var o RenderIndentGuidesOptions
	if len(options) > 0 {
		o = options[0]
	}
	return shiki.Transformer{Name: "@shikijs/transformers:render-indent-guides", Code: func(ctx *shiki.TransformerContext, code *shiki.Node) (*shiki.Node, error) {
		value := o.Indent
		if value == nil {
			value = 2
		}
		raw, _ := ctx.Options.Meta["__raw"].(string)
		if m := indentMeta.FindStringSubmatch(raw); m != nil {
			value = m[1]
		}
		if v, ok := ctx.Options.Meta["indent"]; ok {
			value = v
		}
		indent, err := strconv.Atoi(fmt.Sprint(value))
		if err != nil || indent <= 0 {
			return code, nil
		}
		if indent > 100000 {
			return code, nil
		}
		re := regexp.MustCompile(fmt.Sprintf(` {%d}| {0,%d}\t| {1,}$`, indent, indent-1))
		type emptyLine struct {
			node  *shiki.Node
			level int
		}
		var empty []emptyLine
		level := 0
		for _, line := range code.Children {
			if line.Type != "element" {
				continue
			}
			if len(line.Children) == 0 {
				empty = append(empty, emptyLine{line, level})
				continue
			}
			first := line.Children[0]
			text, ok := textHead(first)
			if !ok {
				empty = append(empty, emptyLine{line, level})
				continue
			}
			blanks := text[:len(text)-len(strings.TrimLeft(text, " \t"))]
			ranges := re.FindAllStringIndex(blanks, -1)
			for _, e := range empty {
				nodes := []*shiki.Node{}
				for i := 0; i < min(len(ranges), e.level+1); i++ {
					n := shiki.Element("span", nil)
					n.SetProperty("class", "indent")
					n.SetProperty("style", fmt.Sprintf("--indent-offset: %dch;", i*indent))
					nodes = append(nodes, n)
				}
				e.node.Children = append(nodes, e.node.Children...)
			}
			empty = nil
			level = len(ranges)
			if len(ranges) > 0 {
				nodes := []*shiki.Node{}
				for _, r := range ranges {
					n := shiki.Element("span", nil, shiki.Text(text[r[0]:r[1]]))
					n.SetProperty("class", "indent")
					nodes = append(nodes, n)
				}
				line.Children = append(nodes, line.Children...)
				first.Children[0].Value = text[ranges[len(ranges)-1][1]:]
			}
		}
		return code, nil
	}}
}
