package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blisspixel/nemalo/internal/app"
	"github.com/blisspixel/nemalo/internal/config"
)

// pipeWriter lets the test read stdout while the server is still writing.
type pipeWriter struct{ w *io.PipeWriter }

func (p pipeWriter) Write(b []byte) (int, error) { return p.w.Write(b) }

func TestMCPServeSpeaksOnlyProtocolOnStdout(t *testing.T) {
	scope := t.TempDir()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	mcpInput = inR
	t.Cleanup(func() { mcpInput = nil })
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- Execute(context.Background(), []string{"mcp", "serve", "--scope", scope}, pipeWriter{outW}, &stderr, app.Service{}, func(string) string { return "" }, func() (config.Paths, error) {
			return config.Paths{Config: filepath.Join(scope, "none"), State: scope}, nil
		}, nil)
		_ = outW.Close()
	}()
	lines := bufio.NewScanner(outR)
	lines.Buffer(make([]byte, 1<<20), 1<<20)
	send := func(msg string) {
		if _, err := io.WriteString(inW, msg+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	receive := func() map[string]any {
		if !lines.Scan() {
			t.Fatal("no protocol response", lines.Err())
		}
		var m map[string]any
		if err := json.Unmarshal(lines.Bytes(), &m); err != nil || m["jsonrpc"] != "2.0" {
			t.Fatalf("stdout carried a non-protocol line: %q", lines.Text())
		}
		return m
	}
	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	init := receive()
	if init["result"] == nil {
		t.Fatal("initialize failed", init)
	}
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	list := receive()
	tools := list["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 18 {
		t.Fatal("tool count", len(tools))
	}
	foundDiff := false
	for _, tool := range tools {
		foundDiff = foundDiff || tool.(map[string]any)["name"] == "collection_diff"
	}
	if !foundDiff {
		t.Fatal("collection diff missing from stdio tools")
	}
	send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"library_status","arguments":{"library":"relative"}}}`)
	call := receive()
	if call["result"].(map[string]any)["isError"] != true {
		t.Fatal("scope violation not reported as a tool error", call)
	}
	_ = inW.Close()
	select {
	case code := <-done:
		if code != 0 || stderr.Len() != 0 {
			t.Fatal("server exit", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop at end of input")
	}
}

func TestMCPServeUsage(t *testing.T) {
	scope := t.TempDir()
	for _, args := range [][]string{
		{"mcp"}, {"mcp", "run"}, {"mcp", "serve", "extra", "--scope", scope},
		{"mcp", "serve", "--transport", "http", "--scope", scope},
		{"mcp", "serve", "--scope", scope, "--json"},
		{"mcp", "serve"},
		{"mcp", "serve", "--scope", "relative"},
		{"search", "x", "--allow-network"}, {"check", "x", "--scope", scope},
	} {
		code, out, stderr := execute(t, args, nil)
		if code != 2 || (args[0] == "mcp" && (out != "" || !strings.Contains(stderr, "nemalo:"))) {
			t.Fatal("invalid mcp usage", args, code, out, stderr)
		}
	}
}

func TestMCPServeSkipsMissingConfiguredFolders(t *testing.T) {
	scope := t.TempDir()
	mcpInput = strings.NewReader("")
	t.Cleanup(func() { mcpInput = nil })
	missing := filepath.Join(scope, "not-created-yet")
	var out, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"mcp", "serve", "--scope", scope}, &out, &stderr, app.Service{}, func(name string) string {
		if name == "NEMALO_REVIEW" {
			return missing
		}
		return ""
	}, func() (config.Paths, error) {
		return config.Paths{Config: filepath.Join(scope, "none"), State: scope}, nil
	}, nil)
	if code != 0 || out.Len() != 0 || !strings.Contains(stderr.String(), "not added to MCP scopes") {
		t.Fatal("a missing configured folder blocked an explicit scope", code, out.String(), stderr.String())
	}
}
