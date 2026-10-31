// Package server exposes the stock checker as MCP tools.
package server

import (
	"context"
	"errors"

	"github.com/GillMacedo/runwaystock/internal/stock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CheckInput is the input of inventory_check.
type CheckInput struct {
	SalesPath    string            `json:"sales_path" jsonschema:"Path to the sales or orders CSV"`
	StockPath    string            `json:"stock_path" jsonschema:"Path to the stock count CSV"`
	LeadDays     *float64          `json:"lead_days,omitempty" jsonschema:"Days from ordering to receiving, default 14. A lead time in the stock file overrides it"`
	CoverDays    *float64          `json:"cover_days,omitempty" jsonschema:"Days of sales each order should cover, default 30"`
	WindowDays   *int              `json:"window_days,omitempty" jsonschema:"Days of recent sales to use, default 90"`
	ServiceLevel *int              `json:"service_level,omitempty" jsonschema:"Chance of not running out during lead time: 90, 95 or 99, default 95"`
	DateFormat   string            `json:"date_format,omitempty" jsonschema:"dmy or mdy, needed only if the tool reports that dates are ambiguous"`
	SalesColumns map[string]string `json:"sales_columns,omitempty" jsonschema:"Column names by field (date, sku, qty), when detection fails"`
	StockColumns map[string]string `json:"stock_columns,omitempty" jsonschema:"Column names by field (sku, qty, lead, cost, moq), when detection fails"`
	Limit        *int              `json:"limit,omitempty" jsonschema:"Most SKUs to return, most urgent first, default 100. 0 returns only the totals"`
}

// CheckOutput is the output of inventory_check.
type CheckOutput struct {
	stock.Result
	LineCount        int   `json:"line_count"`
	Truncated        bool  `json:"truncated"`
	SkippedSalesRows []int `json:"skipped_sales_rows"`
	SkippedStockRows []int `json:"skipped_stock_rows"`
}

// InspectInput is the input of inventory_inspect_csv.
type InspectInput struct {
	Path string `json:"path" jsonschema:"Path to the CSV"`
	Kind string `json:"kind" jsonschema:"sales or stock"`
}

// InspectOutput holds no row contents.
type InspectOutput struct {
	Columns map[string]string `json:"columns"`
	Rows    int               `json:"rows"`
	Skipped int               `json:"skipped"`
}

func no() *bool { value := false; return &value }

// New returns the MCP server.
func New(version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "runwaystock", Version: version}, nil)
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: no(), IdempotentHint: true, OpenWorldHint: no()}

	mcp.AddTool(server, &mcp.Tool{
		Name:  "inventory_check",
		Title: "Check stock and reorder points",
		Description: "Works out what to reorder and how much from a sales CSV and a stock CSV, with reorder points, safety stock, stockout dates and dead stock. " +
			"Use for inventory planning, purchase orders, or finding slow-moving stock. Read-only, and never uses the network. Results include SKUs and quantities from the files, so they reach the model. It can't place orders.",
		Annotations: annotations,
	}, check)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "inventory_inspect_csv",
		Title:       "Inspect a sales or stock CSV",
		Description: "Shows which columns runwaystock detects in a CSV and how many rows it can read, without returning row contents. Use if a file is unfamiliar or a check failed.",
		Annotations: annotations,
	}, inspect)
	return server
}

func check(_ context.Context, _ *mcp.CallToolRequest, in CheckInput) (*mcp.CallToolResult, CheckOutput, error) {
	limit := 100
	if in.Limit != nil {
		limit = *in.Limit
	}
	if limit < 0 || limit > 500 {
		return nil, CheckOutput{}, errors.New("limit must be between 0 and 500")
	}
	settings := stock.Defaults()
	if in.LeadDays != nil {
		settings.LeadDays = *in.LeadDays
	}
	if in.CoverDays != nil {
		settings.CoverDays = *in.CoverDays
	}
	if in.WindowDays != nil {
		settings.WindowDays = *in.WindowDays
	}
	if in.ServiceLevel != nil {
		settings.Service = *in.ServiceLevel
	}
	sales, err := stock.LoadSales(in.SalesPath, in.SalesColumns, in.DateFormat)
	if err != nil {
		return nil, CheckOutput{}, err
	}
	items, err := stock.LoadStock(in.StockPath, in.StockColumns)
	if err != nil {
		return nil, CheckOutput{}, err
	}
	result, err := stock.Check(sales.Rows, items.Items, settings)
	if err != nil {
		return nil, CheckOutput{}, err
	}
	result.Warnings = append(result.Warnings, items.Warnings...)
	out := CheckOutput{Result: result, LineCount: len(result.Lines), SkippedSalesRows: sales.Skipped, SkippedStockRows: items.Skipped}
	if len(out.Lines) > limit {
		out.Lines, out.Truncated = out.Lines[:limit], true
	}
	return nil, out, nil
}

func inspect(_ context.Context, _ *mcp.CallToolRequest, in InspectInput) (*mcp.CallToolResult, InspectOutput, error) {
	switch in.Kind {
	case "sales":
		loaded, err := stock.LoadSales(in.Path, nil, "dmy")
		if err != nil {
			return nil, InspectOutput{}, err
		}
		return nil, InspectOutput{Columns: loaded.Columns, Rows: len(loaded.Rows), Skipped: len(loaded.Skipped)}, nil
	case "stock":
		loaded, err := stock.LoadStock(in.Path, nil)
		if err != nil {
			return nil, InspectOutput{}, err
		}
		return nil, InspectOutput{Columns: loaded.Columns, Rows: len(loaded.Items), Skipped: len(loaded.Skipped)}, nil
	}
	return nil, InspectOutput{}, errors.New("kind must be sales or stock")
}
