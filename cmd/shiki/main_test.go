package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	for _, tc := range []struct {
		args        []string
		input, want string
	}{{[]string{"--lang", "js", "--theme", "github-dark", "--format", "html"}, "const x = 1", `<span style="color:#F97583">const</span>`}, {[]string{"--list-themes"}, "", "github-dark\n"}, {[]string{"--list-langs"}, "", "typescript\n"}, {[]string{"--format", "tokens"}, "hello", `"content":"hello"`}, {[]string{"--version"}, "", "4.4.3"}} {
		var output, errors bytes.Buffer
		if err := run(tc.args, strings.NewReader(tc.input), &output, &errors); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), tc.want) {
			t.Fatalf("%v: %s", tc.args, output.String())
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("const answer = 42"))
	}))
	defer server.Close()
	var output bytes.Buffer
	if err := run([]string{server.URL + "/source.js?test=1", "--format", "html"}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "<pre") {
		t.Fatal(output.String())
	}
	output.Reset()
	if err := run([]string{server.URL + "/Dockerfile", "--format", "html"}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "const answer = 42") {
		t.Fatal(output.String())
	}
	if err := run([]string{server.URL + "/missing"}, strings.NewReader(""), &output, &output); err == nil {
		t.Fatal("HTTP error was ignored")
	}
	makefile := filepath.Join(t.TempDir(), "Makefile")
	if err := os.WriteFile(makefile, []byte("all:\n\techo Hello"), 0644); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run([]string{makefile, "--format", "html"}, strings.NewReader(""), &output, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "echo Hello") {
		t.Fatal(output.String())
	}
	if err := run([]string{"--format", "invalid"}, strings.NewReader(""), &output, &output); err == nil {
		t.Fatal("invalid format accepted")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestCLIReportsCatalogWriteErrors(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"--list-themes"}, {"--list-langs"}} {
		if err := run(args, strings.NewReader(""), failingWriter{}, failingWriter{}); err == nil {
			t.Fatalf("%v: write error was ignored", args)
		}
	}
}

func TestANSIColorEnvironment(t *testing.T) {
	for _, level := range []string{"0", "1", "2", "3"} {
		t.Run(level, func(t *testing.T) {
			t.Setenv("FORCE_COLOR", level)
			var output bytes.Buffer
			if err := run([]string{"--lang", "js"}, strings.NewReader("const x = 1"), &output, &output); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), "\x1b[") != (level != "0") {
				t.Fatal(output.String())
			}
		})
	}
}
