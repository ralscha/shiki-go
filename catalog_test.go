package shiki

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestLanguageCatalogParity(t *testing.T) {
	data, err := os.ReadFile("testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Code, HTML string
		Options          Options
		Tokens           json.RawMessage
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, info := range BundledLanguages() {
		ids = append(ids, info.ID)
	}
	h, err := NewHighlighter(HighlighterOptions{Langs: ids, Themes: []string{"github-dark"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			unlimited := time.Duration(0)
			tc.Options.TokenizeTimeLimit = &unlimited
			result, err := h.CodeToTokens(tc.Code, tc.Options)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var actual, want any
			_ = json.Unmarshal(data, &actual)
			_ = json.Unmarshal(tc.Tokens, &want)
			if !reflect.DeepEqual(actual, want) {
				_ = os.MkdirAll(".cache/mismatches", 0755)
				_ = os.WriteFile(".cache/mismatches/"+tc.Name+".got.json", data, 0644)
				_ = os.WriteFile(".cache/mismatches/"+tc.Name+".want.json", tc.Tokens, 0644)
				t.Errorf("tokens differ; details in .cache/mismatches/%s.{got,want}.json", tc.Name)
			}
			html, err := h.CodeToHTML(tc.Code, tc.Options)
			if err != nil {
				t.Fatal(err)
			}
			if html != tc.HTML {
				t.Error("HTML differs")
			}
		})
	}
}
