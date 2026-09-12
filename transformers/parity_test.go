package transformers

import (
	"encoding/json"
	shiki "github.com/ralscha/shiki-go"
	"os"
	"testing"
)

func closeOnCleanup(t testing.TB, closer interface{ Close() error }) {
	t.Helper()
	t.Cleanup(func() {
		if err := closer.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
}

func TestReferenceParity(t *testing.T) {
	data, err := os.ReadFile("../testdata/transformers.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Transformer, Code, HTML, CSS string
		Options                            shiki.Options
		Config                             json.RawMessage
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	h, err := shiki.NewHighlighter(shiki.HighlighterOptions{Langs: []string{"js", "ts", "tsx", "python", "html", "md", "jinja", "liquid"}, Themes: []string{"github-dark", "github-light", "rose-pine"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			var tr shiki.Transformer
			var registry *StyleToClassTransformer
			o := NotationOptions{}
			if len(tc.Config) > 0 && tc.Config[0] == '{' {
				_ = json.Unmarshal(tc.Config, &o)
			}
			switch tc.Transformer {
			case "colorizedBrackets":
				var o ColorizedBracketsOptions
				_ = json.Unmarshal(tc.Config, &o)
				tr = ColorizedBrackets(o)
			case "notationDiff":
				tr = NotationDiff(o)
			case "notationHighlight":
				tr = NotationHighlight(o)
			case "notationFocus":
				tr = NotationFocus(o)
			case "notationErrorLevel":
				tr = NotationErrorLevel(o)
			case "notationWordHighlight":
				tr = NotationWordHighlight(o)
			case "metaHighlight":
				tr = MetaHighlight()
			case "metaWordHighlight":
				tr = MetaWordHighlight()
			case "removeComments":
				tr = RemoveComments()
			case "removeLineBreak":
				tr = RemoveLineBreak()
			case "removeNotationEscape":
				tr = RemoveNotationEscape()
			case "renderWhitespace":
				var o RenderWhitespaceOptions
				_ = json.Unmarshal(tc.Config, &o)
				tr = RenderWhitespace(o)
			case "renderIndentGuides":
				tr = RenderIndentGuides()
			case "renderLineNumber":
				var o RenderLineNumberOptions
				_ = json.Unmarshal(tc.Config, &o)
				tr = RenderLineNumber(o)
			case "styleToClass":
				registry = StyleToClass()
				tr = registry.Transformer
			case "compactLineOptions":
				var o []CompactLineOption
				_ = json.Unmarshal(tc.Config, &o)
				tr = CompactLineOptions(o)
			default:
				t.Fatalf("unknown transformer %s", tc.Transformer)
			}
			tc.Options.Transformers = []shiki.Transformer{tr}
			html, err := h.CodeToHTML(tc.Code, tc.Options)
			if err != nil {
				t.Fatal(err)
			}
			if html != tc.HTML {
				t.Errorf("HTML mismatch\ngot:  %s\nwant: %s", html, tc.HTML)
			}
			if registry != nil && registry.GetCSS() != tc.CSS {
				t.Errorf("CSS mismatch\ngot:  %s\nwant: %s", registry.GetCSS(), tc.CSS)
			}
		})
	}
}
