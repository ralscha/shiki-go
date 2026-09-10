package stream

import (
	"encoding/json"
	shiki "github.com/ralscha/shiki-go"
	"os"
	"reflect"
	"testing"
)

func TestReferenceParity(t *testing.T) {
	data, err := os.ReadFile("../testdata/stream.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string
		Options shiki.Options
		Chunks  []string
		Results []json.RawMessage
		Closed  json.RawMessage
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	h, err := shiki.NewHighlighter(shiki.HighlighterOptions{Langs: []string{"js", "markdown"}, Themes: []string{"github-dark", "github-light"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)
	compare := func(t *testing.T, actual any, reference json.RawMessage) {
		t.Helper()
		b, err := json.Marshal(actual)
		if err != nil {
			t.Fatal(err)
		}
		var got, want any
		_ = json.Unmarshal(b, &got)
		_ = json.Unmarshal(reference, &want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("JSON mismatch\ngot: %s\nwant: %s", b, reference)
		}
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			tr := NewTokenizer(h, tc.Options)
			for i, chunk := range tc.Chunks {
				result, err := tr.Enqueue(chunk)
				if err != nil {
					t.Fatal(err)
				}
				compare(t, result, tc.Results[i])
			}
			compare(t, tr.Close(), tc.Closed)
		})
	}
}
