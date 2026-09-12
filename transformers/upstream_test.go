package transformers

import (
	shiki "github.com/ralscha/shiki-go"
	"regexp"
	"strings"
	"testing"
)

func TestNotationWordHighlightClassActiveCode(t *testing.T) {
	h, err := shiki.NewHighlighter(shiki.HighlighterOptions{Langs: []string{"js"}, Themes: []string{"github-dark"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)

	t.Run("adds configured classes", func(t *testing.T) {
		html, err := h.CodeToHTML("const hello = 'world' // [!code word:hello]", shiki.Options{
			Lang:  "js",
			Theme: "github-dark",
			Transformers: []shiki.Transformer{NotationWordHighlight(NotationOptions{
				ClassActivePre:  "has-word-highlight-pre",
				ClassActiveCode: "has-word-highlight-code",
			})},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(html, "has-word-highlight-pre") || !strings.Contains(html, `<code class="has-word-highlight-code">`) {
			t.Fatal(html)
		}
	})

	t.Run("does not add classes without notation", func(t *testing.T) {
		html, err := h.CodeToHTML("const hello = 'world'", shiki.Options{
			Lang:         "js",
			Theme:        "github-dark",
			Transformers: []shiki.Transformer{NotationWordHighlight(NotationOptions{ClassActiveCode: "has-word-highlight-code"})},
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(html, "has-word-highlight-code") {
			t.Fatal(html)
		}
	})
}

var lineClass = regexp.MustCompile(`<span class="line([^"]*)">`)

func renderedLineClasses(html string) []string {
	matches := lineClass.FindAllStringSubmatch(html, -1)
	classes := make([]string, len(matches))
	for i, match := range matches {
		classes[i] = strings.TrimSpace(match[1])
	}
	return classes
}

func TestNotationOnContentCommentLines(t *testing.T) {
	h, err := shiki.NewHighlighter(shiki.HighlighterOptions{Langs: []string{"yaml"}, Themes: []string{"github-dark"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)

	t.Run("keeps notation on a comment containing visible content", func(t *testing.T) {
		html, err := h.CodeToHTML("# foo # [!code ++] [!code focus]\nbar", shiki.Options{
			Lang:         "yaml",
			Theme:        "github-dark",
			Transformers: []shiki.Transformer{NotationDiff(), NotationFocus()},
		})
		if err != nil {
			t.Fatal(err)
		}
		classes := renderedLineClasses(html)
		if len(classes) != 2 || !strings.Contains(classes[0], "diff add") || !strings.Contains(classes[0], "focused") || classes[1] != "" {
			t.Fatalf("line classes %q in %s", classes, html)
		}
		if !strings.Contains(html, "# foo") || strings.Contains(html, "[!code") {
			t.Fatal(html)
		}
	})

	t.Run("moves a standalone notation to the next line", func(t *testing.T) {
		html, err := h.CodeToHTML("# [!code ++] [!code focus]\nbar", shiki.Options{
			Lang:         "yaml",
			Theme:        "github-dark",
			Transformers: []shiki.Transformer{NotationDiff(), NotationFocus()},
		})
		if err != nil {
			t.Fatal(err)
		}
		classes := renderedLineClasses(html)
		if len(classes) != 1 || !strings.Contains(classes[0], "diff add") || !strings.Contains(classes[0], "focused") {
			t.Fatalf("line classes %q in %s", classes, html)
		}
		if strings.Contains(html, "[!code") {
			t.Fatal(html)
		}
	})

	t.Run("recognizes standalone custom notation", func(t *testing.T) {
		custom := CreateCommentNotationTransformer("custom-focus", regexp.MustCompile(`\s*@focus`), func(_ *shiki.TransformerContext, _ []string, _ *shiki.Node, _ *shiki.Node, lines []*shiki.Node, index int) bool {
			shiki.AddClassToHAST(lines[index], "focused")
			return true
		}, "v3")
		html, err := h.CodeToHTML("# @focus\nbar", shiki.Options{
			Lang:         "yaml",
			Theme:        "github-dark",
			Transformers: []shiki.Transformer{custom},
		})
		if err != nil {
			t.Fatal(err)
		}
		classes := renderedLineClasses(html)
		if len(classes) != 1 || !strings.Contains(classes[0], "focused") || strings.Contains(html, "@focus") {
			t.Fatalf("line classes %q in %s", classes, html)
		}
	})
}

func TestRenderLineNumber(t *testing.T) {
	h, err := shiki.NewHighlighter(shiki.HighlighterOptions{Langs: []string{"js"}, Themes: []string{"github-dark"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)

	html, err := h.CodeToHTML("const one = 1\nconst two = 2", shiki.Options{
		Lang:         "js",
		Theme:        "github-dark",
		Transformers: []shiki.Transformer{RenderLineNumber()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `<span class="line"><span class="line-number">1</span>`) || !strings.Contains(html, `<span class="line"><span class="line-number">2</span>`) {
		t.Fatal(html)
	}

	zero := 0
	html, err = h.CodeToHTML("const one = 1", shiki.Options{
		Lang:         "js",
		Theme:        "github-dark",
		Transformers: []shiki.Transformer{RenderLineNumber(RenderLineNumberOptions{ClassLineNumber: "gutter", Start: &zero})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, `<span class="line"><span class="gutter">0</span>`) {
		t.Fatal(html)
	}
}
