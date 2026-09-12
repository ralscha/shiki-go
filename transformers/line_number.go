package transformers

import (
	shiki "github.com/ralscha/shiki-go"
	"strconv"
)

// RenderLineNumberOptions configures RenderLineNumber. Start defaults to 1;
// use a non-nil pointer to start at another value, including zero.
type RenderLineNumberOptions struct {
	ClassLineNumber string
	Start           *int
}

// RenderLineNumber prepends each rendered line with a numbered span.
func RenderLineNumber(options ...RenderLineNumberOptions) shiki.Transformer {
	className := "line-number"
	start := 1
	if len(options) > 0 {
		if options[0].ClassLineNumber != "" {
			className = options[0].ClassLineNumber
		}
		if options[0].Start != nil {
			start = *options[0].Start
		}
	}
	return shiki.Transformer{Name: "@shikijs/transformers:render-line-number", Line: func(_ *shiki.TransformerContext, node *shiki.Node, line int) (*shiki.Node, error) {
		if node.TagName == "span" {
			number := shiki.Element("span", nil, shiki.Text(strconv.Itoa(start+line-1)))
			number.SetProperty("class", className)
			node.Children = append([]*shiki.Node{number}, node.Children...)
		}
		return node, nil
	}}
}
