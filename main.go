// Command runwaystock works out what to reorder from sales and stock files.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/GillMacedo/runwaystock/internal/server"
	"github.com/GillMacedo/runwaystock/internal/stock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.2.0"

const usage = `Usage:
  runwaystock check <sales.csv> <stock.csv>   Work out what to reorder
  runwaystock serve                           Run as an MCP server over stdio
  runwaystock version

Options for check:
  --lead <days>            Days from ordering to receiving (default 14)
  --cover <days>           Days of sales each order should cover (default 30)
  --window <days>          Days of recent sales to use (default 90)
  --service 90|95|99       Chance of not running out during lead time (default 95)
  --date-format dmy|mdy    Needed only when dates are ambiguous
  --sales-columns date=When,sku=Item,qty=Sold    Name columns in the sales file
  --stock-columns sku=Item,qty=Count             Name columns in the stock file
  --json                   Print JSON

Reads your files and never changes them. Never uses the network.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 2
	}
	switch args[0] {
	case "serve":
		if err := server.New(version).Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			fmt.Fprintf(stderr, "runwaystock: %v\n", err)
			return 2
		}
		return 0
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "check":
		if err := check(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "runwaystock: %v\n", err)
			return 2
		}
		return 0
	}
	fmt.Fprint(stdout, usage)
	return 2
}

func columnMap(text string) (map[string]string, error) {
	out := map[string]string{}
	if text == "" {
		return out, nil
	}
	for _, pair := range strings.Split(text, ",") {
		field, name, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("column options need field=Column pairs, not %q", pair)
		}
		out[strings.TrimSpace(field)] = strings.TrimSpace(name)
	}
	return out, nil
}

func check(args []string, stdout io.Writer) error {
	settings := stock.Defaults()
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Float64Var(&settings.LeadDays, "lead", settings.LeadDays, "")
	flags.Float64Var(&settings.CoverDays, "cover", settings.CoverDays, "")
	flags.IntVar(&settings.WindowDays, "window", settings.WindowDays, "")
	flags.IntVar(&settings.Service, "service", settings.Service, "")
	dateFormat := flags.String("date-format", "", "")
	salesColumns := flags.String("sales-columns", "", "")
	stockColumns := flags.String("stock-columns", "", "")
	asJSON := flags.Bool("json", false, "")

	// The standard flag package stops at the first path, so options can't
	// follow it without this loop.
	var paths []string
	for rest := args; ; {
		if err := flags.Parse(rest); err != nil {
			return err
		}
		if flags.NArg() == 0 {
			break
		}
		paths = append(paths, flags.Arg(0))
		rest = flags.Args()[1:]
	}
	if len(paths) != 2 {
		return fmt.Errorf("check takes a sales file and a stock file\n%s", usage)
	}
	if *dateFormat != "" && *dateFormat != "dmy" && *dateFormat != "mdy" {
		return fmt.Errorf("--date-format must be dmy or mdy")
	}
	salesMap, err := columnMap(*salesColumns)
	if err != nil {
		return err
	}
	stockMap, err := columnMap(*stockColumns)
	if err != nil {
		return err
	}
	sales, err := stock.LoadSales(paths[0], salesMap, *dateFormat)
	if err != nil {
		return err
	}
	items, err := stock.LoadStock(paths[1], stockMap)
	if err != nil {
		return err
	}
	result, err := stock.Check(sales.Rows, items.Items, settings)
	if err != nil {
		return err
	}
	result.Warnings = append(result.Warnings, items.Warnings...)

	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(struct {
			stock.Result
			SkippedSalesRows []int `json:"skipped_sales_rows"`
			SkippedStockRows []int `json:"skipped_stock_rows"`
		}{result, sales.Skipped, items.Skipped})
	}
	fmt.Fprintf(stdout, "As of %s, using %d days of sales\n", result.AsOf, result.WindowDays)
	for _, line := range result.Lines {
		switch line.Status {
		case stock.OrderNow, stock.Soon:
			cost := ""
			if line.OrderCost != "" {
				cost = "  costs " + line.OrderCost
			}
			fmt.Fprintf(stdout, "%-15s  %s: buy %g%s  (%g on hand, %g a day, out by %s, reorder at %g)\n", strings.ToUpper(line.Status), line.SKU, line.OrderQty, cost, line.OnHand, line.DailySales, line.StockoutDate, line.ReorderPoint)
		case stock.OK:
			fmt.Fprintf(stdout, "%-15s  %s  (%g on hand, %.0f days left)\n", "OK", line.SKU, line.OnHand, line.DaysLeft)
		case stock.Over:
			fmt.Fprintf(stdout, "%-15s  %s  (%g on hand, %.0f days left)\n", "OVERSTOCKED", line.SKU, line.OnHand, line.DaysLeft)
		case stock.Dead:
			value := ""
			if line.StockValue != "" {
				value = ", worth " + line.StockValue
			}
			fmt.Fprintf(stdout, "%-15s  %s  (%g on hand%s)\n", "NO RECENT SALES", line.SKU, line.OnHand, value)
		}
	}
	for _, sku := range result.NoStockCount {
		fmt.Fprintf(stdout, "NOT IN STOCK FILE  %s sold recently\n", sku)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintf(stdout, "WARNING: %s\n", warning)
	}
	for _, row := range sales.Skipped {
		fmt.Fprintf(stdout, "SKIPPED sales row %d\n", row)
	}
	for _, row := range items.Skipped {
		fmt.Fprintf(stdout, "SKIPPED stock row %d\n", row)
	}
	fmt.Fprintf(stdout, "%d to order now, costing %s. %s tied up in stock with no recent sales.\n", result.OrderNow, result.OrderCost, result.DeadValue)
	return nil
}
