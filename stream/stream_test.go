package stream

import (
	"context"
	shiki "github.com/ralscha/shiki-go"
	"io"
	"reflect"
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

func TestOffsetsAcrossCloneCloseAndClear(t *testing.T) {
	h, err := shiki.NewHighlighter(shiki.HighlighterOptions{Themes: []string{"github-dark"}})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, h)
	tokenizer := NewTokenizer(h, shiki.Options{Lang: "text", Theme: "github-dark"})
	first, err := tokenizer.Enqueue("a😀\r\nb")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Stable) != 2 || first.Stable[1].Content != "\n" || first.Stable[1].Offset != 4 || first.Unstable[0].Offset != 5 {
		t.Fatalf("incorrect UTF-16 or CRLF offsets: %#v", first)
	}
	clone := tokenizer.Clone()
	for _, current := range []*Tokenizer{tokenizer, clone} {
		next, err := current.Enqueue("\nc")
		if err != nil {
			t.Fatal(err)
		}
		if len(next.Stable) != 2 || next.Stable[0].Offset != 5 || next.Stable[1].Offset != 6 || next.Unstable[0].Offset != 7 {
			t.Fatalf("continuation lost its offset: %#v", next)
		}
	}
	closed := clone.Close()
	if len(closed) != 1 || closed[0].Content != "c" || closed[0].Offset != 7 {
		t.Fatalf("close lost the final offset: %#v", closed)
	}
	tokenizer.Clear()
	if len(tokenizer.TokensStable()) != 0 || len(tokenizer.TokensUnstable()) != 0 {
		t.Fatal("clear retained tokens")
	}
	for _, current := range []*Tokenizer{tokenizer, clone} {
		next, err := current.Enqueue("fresh")
		if err != nil {
			t.Fatal(err)
		}
		if next.Recall != 0 || len(next.Unstable) != 1 || next.Unstable[0].Offset != 0 {
			t.Fatalf("offset was not reset: %#v", next)
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
