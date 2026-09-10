package shiki_test

import (
	"fmt"
	shiki "shiki-go"
	"shiki-go/transformers"
	"strings"
)

func ExampleCodeToHTML() {
	html, err := shiki.CodeToHTML("const answer = 42", shiki.Options{Lang: "js", Theme: "github-dark"})
	if err != nil {
		panic(err)
	}
	fmt.Println(strings.Contains(html, `<span style="color:#F97583">const</span>`))
	// Output: true
}

func ExampleHighlighter_CodeToTokens() {
	h, err := shiki.NewHighlighter(shiki.HighlighterOptions{Langs: []string{"go"}, Themes: []string{"github-dark"}})
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := h.Close(); err != nil {
			panic(err)
		}
	}()
	result, err := h.CodeToTokens("package main", shiki.Options{Lang: "go", Theme: "github-dark"})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Tokens[0][0].Content)
	// Output: package
}

func ExampleTransformer() {
	html, err := shiki.CodeToHTML("const answer = 42 // [!code highlight]", shiki.Options{Lang: "js", Theme: "github-dark", Transformers: []shiki.Transformer{transformers.NotationHighlight()}})
	if err != nil {
		panic(err)
	}
	fmt.Println(strings.Contains(html, `class="line highlighted"`))
	// Output: true
}
