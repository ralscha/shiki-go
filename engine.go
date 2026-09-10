package shiki

import (
	"shiki-go/internal/oniguruma"
	"shiki-go/internal/textmate"
)

// RegexCapture uses UTF-8 byte offsets; unmatched captures use -1.
type RegexCapture = oniguruma.Capture
type RegexMatch = oniguruma.Match
type RegexScanner = textmate.RegexScanner

// RegexEngine allows applications to provide another compatible regex engine.
// A supplied engine remains owned by its caller; Close the engine after all
// highlighters using it have been closed.
type RegexEngine = textmate.RegexEngine
type onigurumaAdapter struct{ engine *oniguruma.Engine }

func (e *onigurumaAdapter) NewScanner(patterns []string) (RegexScanner, error) {
	return e.engine.NewScanner(patterns)
}
func (e *onigurumaAdapter) Close() error { return e.engine.Close() }
func NewOnigurumaEngine() (RegexEngine, error) {
	engine, err := oniguruma.New()
	if err != nil {
		return nil, err
	}
	return &onigurumaAdapter{engine}, nil
}
