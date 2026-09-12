package transformers

import (
	shiki "github.com/ralscha/shiki-go"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var metaLines = regexp.MustCompile(`\{([\d,-]+)\}`)

func ParseMetaHighlightString(meta string) []int {
	m := metaLines.FindStringSubmatch(meta)
	if m == nil {
		return nil
	}
	var lines []int
	for part := range strings.SplitSeq(m[1], ",") {
		a, b, rangeFound := strings.Cut(part, "-")
		start, err := strconv.Atoi(a)
		if err != nil {
			continue
		}
		if !rangeFound {
			lines = append(lines, start)
			continue
		}
		end, err := strconv.Atoi(b)
		if err == nil && end-start <= 1000000 {
			for i := start; i <= end; i++ {
				lines = append(lines, i)
			}
		}
	}
	return lines
}

type MetaHighlightOptions struct {
	ClassName   string
	ZeroIndexed bool
}

func MetaHighlight(options ...MetaHighlightOptions) shiki.Transformer {
	var o MetaHighlightOptions
	if len(options) > 0 {
		o = options[0]
	}
	if o.ClassName == "" {
		o.ClassName = "highlighted"
	}
	return shiki.Transformer{Name: "@shikijs/transformers:meta-highlight", Line: func(ctx *shiki.TransformerContext, node *shiki.Node, line int) (*shiki.Node, error) {
		raw, _ := ctx.Options.Meta["__raw"].(string)
		if o.ZeroIndexed {
			line--
		}
		if slices.Contains(ParseMetaHighlightString(raw), line) {
			shiki.AddClassToHAST(node, o.ClassName)
		}
		return node, nil
	}}
}

var metaWords = regexp.MustCompile(`/((?:\\.|[^/])+)/`)
var unescape = regexp.MustCompile(`\\(.)`)

func ParseMetaHighlightWords(meta string) []string {
	out := []string{}
	for _, m := range metaWords.FindAllStringSubmatch(meta, -1) {
		out = append(out, unescape.ReplaceAllString(m[1], "$1"))
	}
	return out
}
func FindAllSubstringIndexes(text, sub string) []int {
	out := []int{}
	if sub == "" {
		return out
	}
	for cursor := 0; cursor < len(text); {
		index := strings.Index(text[cursor:], sub)
		if index < 0 {
			break
		}
		index += cursor
		out = append(out, shiki.UTF16Len(text[:index]))
		cursor = index + len(sub)
	}
	return out
}

type WordHighlightOptions struct{ ClassName string }

func MetaWordHighlight(options ...WordHighlightOptions) shiki.Transformer {
	class := "highlighted-word"
	if len(options) > 0 && options[0].ClassName != "" {
		class = options[0].ClassName
	}
	return shiki.Transformer{Name: "@shikijs/transformers:meta-word-highlight", Preprocess: func(ctx *shiki.TransformerContext, code string) (string, error) {
		raw, _ := ctx.Options.Meta["__raw"].(string)
		for _, word := range ParseMetaHighlightWords(raw) {
			for _, index := range FindAllSubstringIndexes(code, word) {
				ctx.Options.Decorations = append(ctx.Options.Decorations, shiki.Decoration{Start: index, End: index + shiki.UTF16Len(word), Properties: map[string]any{"class": class}})
			}
		}
		return code, nil
	}}
}

type CompactLineOption struct {
	Line    int
	Classes []string
}

func CompactLineOptions(options []CompactLineOption) shiki.Transformer {
	return shiki.Transformer{Name: "@shikijs/transformers:compact-line-options", Line: func(_ *shiki.TransformerContext, node *shiki.Node, line int) (*shiki.Node, error) {
		for _, o := range options {
			if o.Line == line {
				shiki.AddClassToHAST(node, o.Classes...)
				break
			}
		}
		return node, nil
	}}
}

var wordNotation = regexp.MustCompile(`\s*\[!code word:((?:\\.|[^:\]])+)(:\d+)?\]`)

func NotationWordHighlight(options ...NotationOptions) shiki.Transformer {
	o := notationOptions(options)
	if o.ClassActiveWord == "" {
		o.ClassActiveWord = "highlighted-word"
	}
	return CreateCommentNotationTransformer("@shikijs/transformers:notation-highlight-word", wordNotation, func(ctx *shiki.TransformerContext, m []string, _ *shiki.Node, comment *shiki.Node, lines []*shiki.Node, index int) bool {
		count := len(lines)
		if m[2] != "" {
			count, _ = strconv.Atoi(m[2][1:])
		}
		word := unescape.ReplaceAllString(m[1], "$1")
		for i := max(0, index); i < min(index+count, len(lines)); i++ {
			highlightWord(lines[i], comment, word, o.ClassActiveWord)
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
func highlightWord(line, ignored *shiki.Node, word, class string) {
	if word == "" {
		return
	}
	content := shiki.TextContent(line)
	for cursor := 0; cursor < len(content); {
		index := strings.Index(content[cursor:], word)
		if index < 0 {
			break
		}
		index += cursor
		cursor = index + 1
		end := index + len(word)
		offset := 0
		var children []*shiki.Node
		for _, n := range line.Children {
			text, ok := textHead(n)
			if !ok || n.TagName != "span" || n == ignored {
				children = append(children, n)
				continue
			}
			start := max(0, index-offset)
			stop := min(len(text), end-offset)
			if stop > start {
				if start > 0 {
					children = append(children, cloneNode(n, text[:start]))
				}
				middle := cloneNode(n, text[start:stop])
				shiki.AddClassToHAST(middle, class)
				children = append(children, middle)
				if stop < len(text) {
					children = append(children, cloneNode(n, text[stop:]))
				}
			} else {
				children = append(children, n)
			}
			offset += len(text)
		}
		line.Children = children
	}
}
