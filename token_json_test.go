package shiki

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTokenJSONRoundTrip(t *testing.T) {
	for _, input := range []string{
		`{"content":"code","offset":0,"color":"#ffffff","fontStyle":0}`,
		`{"content":"code","offset":0,"fontStyle":0}`,
		`{"content":"code","offset":0,"htmlStyle":{"font-weight":"bold","color":"red"},"htmlAttrs":{"title":"first","class":"second"}}`,
		`{"content":"code","offset":0,"htmlStyle":"color:red; font-weight:bold;"}`,
		`{"content":"code","offset":0,"variants":{"light":{"color":"#000000","fontStyle":0},"dark":{"color":"#ffffff","fontStyle":0}}}`,
	} {
		var token Token
		if err := json.Unmarshal([]byte(input), &token); err != nil {
			t.Fatal(err)
		}
		assertReferenceJSON(t, token, json.RawMessage(input))
		if strings.Contains(input, `"htmlStyle"`) {
			root, err := TokensToHAST(&TokensResult{Tokens: [][]Token{{token}}, FG: "#000", BG: "#fff", ThemeName: "custom"}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			html := HASTToHTML(root)
			if strings.Contains(input, `"htmlStyle":"`) {
				if !strings.Contains(html, `style="color:red; font-weight:bold;"`) {
					t.Fatal(html)
				}
			} else if !strings.Contains(html, `title="first" class="second" style="font-weight:bold;color:red"`) {
				t.Fatal(html)
			}
		}
	}
}
