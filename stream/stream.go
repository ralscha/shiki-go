// Package stream incrementally tokenizes code using Shiki's stable/unstable
// token protocol. Tokens on the unfinished last line may be recalled.
package stream

import (
	"context"
	"fmt"
	shiki "github.com/ralscha/shiki-go"
	"io"
	"strings"
	"sync"
	"unicode/utf8"
)

type Result struct {
	Recall   int           `json:"recall"`
	Stable   []shiki.Token `json:"stable"`
	Unstable []shiki.Token `json:"unstable"`
}
type Tokenizer struct {
	mu               sync.Mutex
	highlighter      *shiki.Highlighter
	options          shiki.Options
	stable, unstable []shiki.Token
	chunk            string
	state            *shiki.GrammarState
}

func NewTokenizer(highlighter *shiki.Highlighter, options shiki.Options) *Tokenizer {
	return &Tokenizer{highlighter: highlighter, options: options, stable: []shiki.Token{}, unstable: []shiki.Token{}}
}
func (t *Tokenizer) Enqueue(chunk string) (Result, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.highlighter == nil {
		return Result{}, fmt.Errorf("shiki: stream requires a highlighter")
	}
	lines := strings.Split(t.chunk+chunk, "\n")
	result := Result{Recall: len(t.unstable), Stable: []shiki.Token{}, Unstable: []shiki.Token{}}
	state := t.state
	for i, line := range lines {
		options := t.options
		options.GrammarState = state
		tokens, err := t.highlighter.CodeToTokens(line, options)
		if err != nil {
			return Result{}, err
		}
		if i+1 < len(lines) {
			state = tokens.GrammarState
			result.Stable = append(result.Stable, tokens.Tokens[0]...)
			result.Stable = append(result.Stable, shiki.Token{Content: "\n", Offset: 0})
		} else {
			result.Unstable = tokens.Tokens[0]
		}
	}
	t.state = state
	t.chunk = lines[len(lines)-1]
	t.stable = append(t.stable, result.Stable...)
	t.unstable = append([]shiki.Token{}, result.Unstable...)
	return result, nil
}
func (t *Tokenizer) Close() []shiki.Token {
	t.mu.Lock()
	defer t.mu.Unlock()
	stable := append([]shiki.Token{}, t.unstable...)
	t.unstable = []shiki.Token{}
	t.chunk = ""
	t.state = nil
	return stable
}
func (t *Tokenizer) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stable = []shiki.Token{}
	t.unstable = []shiki.Token{}
	t.chunk = ""
	t.state = nil
}
func (t *Tokenizer) Clone() *Tokenizer {
	t.mu.Lock()
	defer t.mu.Unlock()
	clone := NewTokenizer(t.highlighter, t.options)
	clone.stable = append([]shiki.Token{}, t.stable...)
	clone.unstable = append([]shiki.Token{}, t.unstable...)
	clone.chunk = t.chunk
	clone.state = t.state
	return clone
}
func (t *Tokenizer) TokensStable() []shiki.Token {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]shiki.Token{}, t.stable...)
}
func (t *Tokenizer) TokensUnstable() []shiki.Token {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]shiki.Token{}, t.unstable...)
}

type Event struct {
	Token  *shiki.Token `json:"token,omitempty"`
	Recall int          `json:"recall,omitempty"`
}

// Transform reads UTF-8 code and emits stable tokens, plus optional recalls and
// provisional tokens. emit may return an error to stop consumption.
func Transform(ctx context.Context, reader io.Reader, tokenizer *Tokenizer, allowRecalls bool, emit func(Event) error) error {
	buffer := make([]byte, 4096)
	pending := []byte{}
	send := func(tokens []shiki.Token) error {
		for i := range tokens {
			token := tokens[i]
			if err := emit(Event{Token: &token}); err != nil {
				return err
			}
		}
		return nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			pending = append(pending, buffer[:n]...)
			end := len(pending)
			if readErr == nil {
				end = 0
				for end < len(pending) && utf8.FullRune(pending[end:]) {
					_, size := utf8.DecodeRune(pending[end:])
					end += size
				}
			}
			if end > 0 {
				result, err := tokenizer.Enqueue(string(pending[:end]))
				if err != nil {
					return err
				}
				pending = append([]byte(nil), pending[end:]...)
				if allowRecalls && result.Recall > 0 {
					if err := emit(Event{Recall: result.Recall}); err != nil {
						return err
					}
				}
				if err := send(result.Stable); err != nil {
					return err
				}
				if allowRecalls {
					if err := send(result.Unstable); err != nil {
						return err
					}
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				return readErr
			}
			break
		}
	}
	if len(pending) > 0 {
		result, err := tokenizer.Enqueue(string(pending))
		if err != nil {
			return err
		}
		if allowRecalls && result.Recall > 0 {
			if err := emit(Event{Recall: result.Recall}); err != nil {
				return err
			}
		}
		if err := send(result.Stable); err != nil {
			return err
		}
		if allowRecalls {
			if err := send(result.Unstable); err != nil {
				return err
			}
		}
	}
	final := tokenizer.Close()
	if !allowRecalls {
		return send(final)
	}
	return nil
}
