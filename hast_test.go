package shiki

import (
	"encoding/json"
	"os"
	"testing"
)

func TestHASTReferenceParity(t *testing.T) {
	data, err := os.ReadFile("testdata/hast.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, HTML string
		Node       *Node
		Options    HASTOptions
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if actual := HASTToHTML(tc.Node, tc.Options); actual != tc.HTML {
				t.Errorf("HTML mismatch\ngot: %s\nwant: %s", actual, tc.HTML)
			}
		})
	}
}
