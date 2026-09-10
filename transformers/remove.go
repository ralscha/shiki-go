package transformers

import (
	"fmt"
	shiki "shiki-go"
	"strings"
)

type RemoveCommentsOptions struct{ RemoveEmptyLines *bool }

func RemoveComments(options ...RemoveCommentsOptions) shiki.Transformer {
	removeEmpty := true
	if len(options) > 0 && options[0].RemoveEmptyLines != nil {
		removeEmpty = *options[0].RemoveEmptyLines
	}
	return shiki.Transformer{Name: "@shikijs/transformers:remove-comments", Preprocess: func(ctx *shiki.TransformerContext, code string) (string, error) {
		if ctx.Options.IncludeExplanation != true && ctx.Options.IncludeExplanation != "scopeName" {
			return "", fmt.Errorf("shiki: RemoveComments requires IncludeExplanation true or scopeName")
		}
		return code, nil
	}, Tokens: func(_ *shiki.TransformerContext, tokens [][]shiki.Token) ([][]shiki.Token, error) {
		out := [][]shiki.Token{}
		for _, line := range tokens {
			filtered := []shiki.Token{}
			hasComment := false
			for _, token := range line {
				comment := false
				for _, e := range token.Explanation {
					for _, s := range e.Scopes {
						if strings.HasPrefix(s.ScopeName, "comment") {
							comment = true
						}
					}
				}
				if comment {
					hasComment = true
				} else {
					filtered = append(filtered, token)
				}
			}
			empty := true
			for _, t := range filtered {
				if strings.TrimSpace(t.Content) != "" {
					empty = false
				}
			}
			if removeEmpty && hasComment && empty {
				continue
			}
			out = append(out, filtered)
		}
		return out, nil
	}}
}
func RemoveLineBreak() shiki.Transformer {
	return shiki.Transformer{Name: "@shikijs/transformers:remove-line-break", Code: func(_ *shiki.TransformerContext, code *shiki.Node) (*shiki.Node, error) {
		out := []*shiki.Node{}
		for _, n := range code.Children {
			if n.Type != "text" || n.Value != "\n" {
				out = append(out, n)
			}
		}
		code.Children = out
		return nil, nil
	}}
}
func RemoveNotationEscape() shiki.Transformer {
	return shiki.Transformer{Name: "@shikijs/transformers:remove-notation-escape", Code: func(_ *shiki.TransformerContext, code *shiki.Node) (*shiki.Node, error) {
		var visit func(*shiki.Node)
		visit = func(n *shiki.Node) {
			if n.Type == "text" {
				n.Value = strings.Replace(n.Value, `[\!code`, `[!code`, 1)
			} else {
				for _, c := range n.Children {
					visit(c)
				}
			}
		}
		visit(code)
		return code, nil
	}}
}
