package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GillMacedo/runwaystock/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const salesFile = "examples/sales.csv"
const stockFile = "examples/stock.csv"

func TestPluginIconIsBundled(t *testing.T) {
	manifestData, err := os.ReadFile("plugin/.claude-plugin/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Icon string `json:"icon"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Icon == "" || filepath.IsAbs(manifest.Icon) {
		t.Fatalf("expected a relative plugin icon path, got %q", manifest.Icon)
	}
	iconData, err := os.ReadFile(filepath.Join("plugin", manifest.Icon))
	if err != nil {
		t.Fatalf("read plugin icon %q: %v", manifest.Icon, err)
	}
	var icon struct {
		XMLName xml.Name `xml:"svg"`
	}
	if err := xml.Unmarshal(iconData, &icon); err != nil {
		t.Fatalf("parse plugin icon %q: %v", manifest.Icon, err)
	}
}

func TestCheckPrints(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{"check", salesFile, stockFile}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	for _, want := range []string{"ORDER NOW", "WIDGET-RED", "NO RECENT SALES", "TSHIRT-UNSOLD", "OVERSTOCKED", "NOT IN STOCK FILE  STICKER-NEW", "SKIPPED sales row"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

func TestOptionsCanFollowThePaths(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{"check", salesFile, stockFile, "--json", "--lead", "5"}, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	var body map[string]any
	if err := json.Unmarshal(out.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body["lines"].([]any)) != 7 {
		t.Errorf("got %v", body["lines"])
	}
}

func TestErrorsExitTwo(t *testing.T) {
	var out, errs bytes.Buffer
	for _, args := range [][]string{{"check"}, {"check", salesFile}, {"check", "/no/such.csv", stockFile}, {"nope"}, {}, {"check", salesFile, stockFile, "--service", "50"}, {"check", salesFile, stockFile, "--sales-columns", "oops"}, {"check", salesFile, stockFile, "--date-format", "x"}} {
		if code := run(args, &out, &errs); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	clientSide, serverSide := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := server.New("test").Connect(ctx, serverSide, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func call(t *testing.T, name string, args map[string]any) (map[string]any, *mcp.CallToolResult) {
	t.Helper()
	result, err := connect(t).CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{}
	if !result.IsError {
		data, _ := json.Marshal(result.StructuredContent)
		json.Unmarshal(data, &body)
	}
	return body, result
}

func TestServerListsReadOnlyTools(t *testing.T) {
	list, err := connect(t).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 2 {
		t.Fatalf("got %d tools", len(list.Tools))
	}
	for _, tool := range list.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s isn't read-only", tool.Name)
		}
	}
}

func TestServerChecks(t *testing.T) {
	body, _ := call(t, "inventory_check", map[string]any{"sales_path": salesFile, "stock_path": stockFile})
	if body["line_count"] != float64(7) {
		t.Errorf("got %v", body)
	}
	body, _ = call(t, "inventory_check", map[string]any{"sales_path": salesFile, "stock_path": stockFile, "limit": 0})
	if body["truncated"] != true || len(body["lines"].([]any)) != 0 {
		t.Errorf("got %v", body)
	}
}

func TestServerInspectHasNoRowContents(t *testing.T) {
	body, result := call(t, "inventory_inspect_csv", map[string]any{"path": stockFile, "kind": "stock"})
	if body["rows"] != float64(7) {
		t.Errorf("got %v", body)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), "WIDGET-RED") {
		t.Error("inspect returned row contents")
	}
}

func TestServerBadPathIsAToolError(t *testing.T) {
	_, result := call(t, "inventory_check", map[string]any{"sales_path": "/no/such.csv", "stock_path": stockFile})
	if !result.IsError {
		t.Error("want a tool error")
	}
}

func TestSourceHasNoNetworkCode(t *testing.T) {
	for _, file := range []string{"main.go", "internal/stock/load.go", "internal/stock/check.go", "internal/stock/parse.go", "internal/server/server.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{`"net/http"`, `"net"`} {
			if strings.Contains(string(data), banned) {
				t.Errorf("%s imports %s", file, banned)
			}
		}
	}
}
