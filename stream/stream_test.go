package stream

import (
	"context"
	"io"
	"reflect"
	shiki "shiki-go"
	"strings"
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

func TestChunkingAndRecalls(t *testing.T) {
	h, err := shiki.NewHighlighter(shiki.HighlighterOptions{Langs: []string{"js"}, Themes: []string{"github-dark"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)
	options := shiki.Options{Lang: "js", Theme: "github-dark"}
	code := "const text = `hello 😀\n${42}`\n/* comment\ncontinued */"
	expected := NewTokenizer(h, options)
	whole, err := expected.Enqueue(code)
	if err != nil {
		t.Fatal(err)
	}
	want := append(whole.Stable, whole.Unstable...)
	tokenizer := NewTokenizer(h, options)
	var actual []shiki.Token
	for _, r := range code {
		result, err := tokenizer.Enqueue(string(r))
		if err != nil {
			t.Fatal(err)
		}
		actual = actual[:len(actual)-result.Recall]
		actual = append(actual, result.Stable...)
		actual = append(actual, result.Unstable...)
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("incremental tokens differ\ngot: %#v\nwant: %#v", actual, want)
	}
	clone := tokenizer.Clone()
	if !reflect.DeepEqual(clone.Close(), tokenizer.Close()) {
		t.Fatal("clone lost unfinished line")
	}
	tokenizer.Clear()
	if len(tokenizer.TokensStable()) != 0 {
		t.Fatal("clear did not reset")
	}
	for _, recalls := range []bool{false, true} {
		var emitted []shiki.Token
		reader := &byteReader{data: []byte(code)}
		err := Transform(context.Background(), reader, NewTokenizer(h, options), recalls, func(e Event) error {
			if e.Recall > 0 {
				emitted = emitted[:len(emitted)-e.Recall]
			} else {
				emitted = append(emitted, *e.Token)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(emitted, want) {
			var b strings.Builder
			for _, token := range emitted {
				b.WriteString(token.Content)
			}
			t.Fatalf("stream corrupted input (recalls %v): %q", recalls, b.String())
		}
	}
}

type byteReader struct{ data []byte }

func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}
