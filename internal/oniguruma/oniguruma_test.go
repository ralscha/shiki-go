package oniguruma

import "testing"

func closeOnCleanup(t testing.TB, closer interface{ Close() error }) {
	t.Helper()
	t.Cleanup(func() {
		if err := closer.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
}

func TestScanner(t *testing.T) {
	e, err := New()
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, e)
	s, err := e.NewScanner([]string{`(?<=foo)(bar)`, `😀`, `\Afirst`, `\Gnext`})
	if err != nil {
		t.Fatal(err)
	}
	closeOnCleanup(t, s)
	for _, tt := range []struct {
		text                          string
		start, opt, index, begin, end int
	}{
		{"foobar", 0, 0, 0, 3, 6}, {"a😀b", 0, 0, 1, 1, 5}, {"first", 0, 0, 2, 0, 5}, {"next", 0, 0, 3, 0, 4},
	} {
		m, err := s.Find(tt.text, tt.start, tt.opt)
		if err != nil {
			t.Fatal(err)
		}
		if m == nil || m.Index != tt.index || m.Captures[0] != (Capture{tt.begin, tt.end}) {
			t.Fatalf("%q: %#v", tt.text, m)
		}
	}
	for _, tt := range []struct {
		text string
		opt  int
	}{{"first", 1}, {"next", 4}} {
		m, err := s.Find(tt.text, 0, tt.opt)
		if err != nil || m != nil {
			t.Fatalf("anchor %q: %#v %v", tt.text, m, err)
		}
	}
}
