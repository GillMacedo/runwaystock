// Package stock works out what to reorder from sales history and stock counts.
package stock

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const maxFileBytes = 50_000_000

// Sale is one sales row. Returns have negative quantities.
type Sale struct {
	Row  int
	Date time.Time
	SKU  string
	Qty  float64
}

// Item is one stock row, summed across locations.
type Item struct {
	Row      int
	SKU      string
	OnHand   float64
	LeadDays float64
	Cost     int64
	HasCost  bool
	MOQ      float64
}

var salesNames = map[string][]string{
	"date": {"date", "order date", "sale date", "created at", "paid at", "sold at", "transaction date", "data", "data venda", "fecha"},
	"sku":  {"sku", "item sku", "variant sku", "lineitem sku", "product sku", "item", "product", "product id", "item id", "lineitem name", "product name", "item name", "produto", "codigo", "articulo"},
	"qty":  {"quantity", "qty", "units", "units sold", "lineitem quantity", "sold", "quantidade", "cantidad"},
}

var stockNames = map[string][]string{
	"sku":  salesNames["sku"],
	"qty":  {"on hand", "onhand", "qty on hand", "stock", "in stock", "available", "inventory", "quantity", "qty", "current stock", "estoque", "saldo", "existencias"},
	"lead": {"lead time", "lead time days", "lead days", "leadtime", "prazo", "prazo entrega"},
	"cost": {"unit cost", "cost", "cost price", "custo", "costo"},
	"moq":  {"moq", "min order", "minimum order", "minimum order quantity", "pedido minimo"},
}

var accents = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "ç", "c", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ü", "u", "ñ", "n")

func normalHeader(cell string) string {
	cell = accents.Replace(strings.ToLower(strings.TrimSpace(cell)))
	return strings.Join(strings.Fields(strings.NewReplacer("_", " ", ".", " ", ":", " ", "(", " ", ")", " ").Replace(cell)), " ")
}

func readRows(path string) ([][]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("can't read %s: %w", path, errors.Unwrap(err))
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a folder. Pass a CSV file", path)
	}
	if info.Size() > maxFileBytes {
		return nil, fmt.Errorf("%s is larger than 50 MB", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("can't read %s: %w", path, err)
	}
	text := decode(data)
	reader := csv.NewReader(strings.NewReader(text))
	reader.Comma = detectDelimiter(text)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("can't parse %s as CSV: %w", path, err)
	}
	return rows, nil
}

func detect(header []string, names map[string][]string, overrides map[string]string) (map[string]int, error) {
	found := map[string]int{}
	normal := make([]string, len(header))
	for i, cell := range header {
		normal[i] = normalHeader(cell)
	}
	for field, candidates := range names {
		for _, candidate := range candidates {
			if _, set := found[field]; set {
				break
			}
			for i, cell := range normal {
				if cell == candidate {
					found[field] = i
					break
				}
			}
		}
	}
	for field, name := range overrides {
		if _, known := names[field]; !known {
			return nil, fmt.Errorf("unknown field %q", field)
		}
		index := -1
		for i, cell := range normal {
			if cell == normalHeader(name) {
				index = i
			}
		}
		if index < 0 {
			return nil, fmt.Errorf("no column named %q. Columns found: %s", name, strings.Join(header, ", "))
		}
		found[field] = index
	}
	return found, nil
}

// findHeader takes the first of the first 20 rows that names the required fields.
func findHeader(path string, rows [][]string, names map[string][]string, overrides map[string]string, required ...string) (int, map[string]int, error) {
	for i := 0; i < len(rows) && i < 20; i++ {
		found, err := detect(rows[i], names, overrides)
		if err != nil {
			continue
		}
		complete := true
		for _, field := range required {
			if _, ok := found[field]; !ok {
				complete = false
			}
		}
		if complete {
			return i, found, nil
		}
	}
	if len(rows) > 0 && len(overrides) > 0 {
		if _, err := detect(rows[0], names, overrides); err != nil {
			return 0, nil, err
		}
	}
	first := "(empty file)"
	if len(rows) > 0 {
		first = strings.Join(rows[0], ", ")
	}
	return 0, nil, fmt.Errorf("can't find %s columns in %s. First row: %s. Name them with a column map", strings.Join(required, ", "), path, first)
}

func cellAt(row []string, columns map[string]int, field string) string {
	if index, ok := columns[field]; ok && index < len(row) {
		return strings.TrimSpace(row[index])
	}
	return ""
}

func parseQty(raw string) (float64, bool) {
	cents, ok := ParseAmount(raw)
	return float64(cents) / 100, ok
}

// Sales is a loaded sales file.
type Sales struct {
	Rows       []Sale
	Skipped    []int
	Columns    map[string]string
	DateFormat string
}

// LoadSales reads a sales or order export.
func LoadSales(path string, overrides map[string]string, dateFormat string) (*Sales, error) {
	rows, err := readRows(path)
	if err != nil {
		return nil, err
	}
	index, columns, err := findHeader(path, rows, salesNames, overrides, "date", "sku", "qty")
	if err != nil {
		return nil, err
	}
	body := rows[index+1:]
	var dates []string
	for _, row := range body {
		dates = append(dates, cellAt(row, columns, "date"))
	}
	if dateFormat == "" {
		dateFormat = DetectDateFormat(dates)
	}
	if dateFormat == "" {
		return nil, fmt.Errorf("the dates in %s could be day-first or month-first. Pass a date format of dmy or mdy", path)
	}
	sales := &Sales{Skipped: []int{}, DateFormat: dateFormat, Columns: named(rows[index], columns)}
	for offset, row := range body {
		number := index + offset + 2
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		date, okDate := ParseDate(cellAt(row, columns, "date"), dateFormat)
		qty, okQty := parseQty(cellAt(row, columns, "qty"))
		sku := cellAt(row, columns, "sku")
		if !okDate || !okQty || sku == "" {
			sales.Skipped = append(sales.Skipped, number)
			continue
		}
		sales.Rows = append(sales.Rows, Sale{Row: number, Date: date, SKU: sku, Qty: qty})
	}
	return sales, nil
}

// Stock is a loaded stock file.
type Stock struct {
	Items    []Item
	Skipped  []int
	Columns  map[string]string
	Warnings []string
}

// LoadStock reads a stock count. Repeated SKUs are summed, since they're
// usually one product in several locations.
func LoadStock(path string, overrides map[string]string) (*Stock, error) {
	rows, err := readRows(path)
	if err != nil {
		return nil, err
	}
	index, columns, err := findHeader(path, rows, stockNames, overrides, "sku", "qty")
	if err != nil {
		return nil, err
	}
	stock := &Stock{Skipped: []int{}, Warnings: []string{}, Columns: named(rows[index], columns)}
	seen := map[string]int{}
	for offset, row := range rows[index+1:] {
		number := index + offset + 2
		if strings.TrimSpace(strings.Join(row, "")) == "" {
			continue
		}
		sku := cellAt(row, columns, "sku")
		qty, ok := parseQty(cellAt(row, columns, "qty"))
		if sku == "" || !ok {
			stock.Skipped = append(stock.Skipped, number)
			continue
		}
		if qty < 0 {
			stock.Warnings = append(stock.Warnings, fmt.Sprintf("%s has negative stock (%g), counted as 0.", sku, qty))
			qty = 0
		}
		item := Item{Row: number, SKU: sku, OnHand: qty}
		if lead, ok := parseQty(cellAt(row, columns, "lead")); ok && lead > 0 {
			item.LeadDays = lead
		}
		if cost, ok := ParseAmount(cellAt(row, columns, "cost")); ok && cost >= 0 {
			item.Cost, item.HasCost = cost, true
		}
		if moq, ok := parseQty(cellAt(row, columns, "moq")); ok && moq > 0 {
			item.MOQ = moq
		}
		key := skuKey(sku)
		if at, repeat := seen[key]; repeat {
			stock.Items[at].OnHand += qty
			stock.Warnings = append(stock.Warnings, fmt.Sprintf("%s appears more than once in the stock file, so its counts were added.", sku))
			continue
		}
		seen[key] = len(stock.Items)
		stock.Items = append(stock.Items, item)
	}
	return stock, nil
}

func named(header []string, columns map[string]int) map[string]string {
	out := map[string]string{}
	for field, index := range columns {
		out[field] = header[index]
	}
	return out
}

func skuKey(sku string) string {
	return strings.ToUpper(strings.TrimSpace(sku))
}
