package stock

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func date(text string) time.Time {
	parsed, err := time.Parse("2006-01-02", text)
	if err != nil {
		panic(err)
	}
	return parsed
}

func steady(sku string, perDay float64, days int) []Sale {
	var out []Sale
	for i := 0; i < days; i++ {
		out = append(out, Sale{Row: i, Date: date("2026-01-01").AddDate(0, 0, i), SKU: sku, Qty: perDay})
	}
	return out
}

func one(t *testing.T, r Result) Line {
	t.Helper()
	if len(r.Lines) != 1 {
		t.Fatalf("want one line, got %+v", r.Lines)
	}
	return r.Lines[0]
}

func settings() Settings {
	return Settings{LeadDays: 10, CoverDays: 30, WindowDays: 90, Service: 95}
}

// Worked by hand: 2 a day with no variation means no safety stock, a reorder
// point of 2 x 10 = 20, and an order up to 2 x (10 + 30) = 80.
func TestSteadyDemandByHand(t *testing.T) {
	r, err := Check(steady("A", 2, 90), []Item{{SKU: "A", OnHand: 15}}, settings())
	if err != nil {
		t.Fatal(err)
	}
	line := one(t, r)
	if line.DailySales != 2 || line.SafetyStock != 0 || line.ReorderPoint != 20 || line.Status != OrderNow || line.OrderQty != 65 {
		t.Errorf("got %+v", line)
	}
	if line.DaysLeft != 7.5 || line.StockoutDate != "2026-04-07" {
		t.Errorf("got %+v", line)
	}
}

// Worked by hand: sales of 0, 4, 0, 4 have mean 2 and sample deviation 2.3094.
// Safety = 1.65 x 2.3094 x sqrt(9) = 11.43, so 12 after rounding up. Reorder
// point = 2 x 9 + 11.43 = 29.43, so 30. Order = 2 x 39 + 11.43 - 10 = 79.43, so 80.
func TestVariableDemandByHand(t *testing.T) {
	var sales []Sale
	for i := 0; i < 4; i++ {
		sales = append(sales, Sale{Date: date("2026-01-01").AddDate(0, 0, i), SKU: "A", Qty: []float64{0, 4, 0, 4}[i]})
	}
	s := settings()
	s.LeadDays = 9
	r, err := Check(sales, []Item{{SKU: "A", OnHand: 10}}, s)
	if err != nil {
		t.Fatal(err)
	}
	line := one(t, r)
	if line.SafetyStock != 12 || line.ReorderPoint != 30 || line.OrderQty != 80 {
		t.Errorf("got %+v", line)
	}
	if !slices.Contains(r.Warnings, "Only 4 days of sales history, so these figures are rough.") {
		t.Errorf("warnings %v", r.Warnings)
	}
}

func TestStatuses(t *testing.T) {
	status := func(onHand float64) string {
		r, err := Check(steady("A", 1, 90), []Item{{SKU: "A", OnHand: onHand}}, settings())
		if err != nil {
			t.Fatal(err)
		}
		return one(t, r).Status
	}
	cases := map[float64]string{5: OrderNow, 10: OrderNow, 11: Soon, 17: Soon, 18: OK, 180: OK, 181: Over}
	for onHand, want := range cases {
		if got := status(onHand); got != want {
			t.Errorf("%g: got %s, want %s", onHand, got, want)
		}
	}
}

func TestDeadStock(t *testing.T) {
	sales := append(steady("A", 1, 10), Sale{Date: date("2025-01-01"), SKU: "OLD", Qty: 5})
	s := settings()
	s.WindowDays = 10
	r, err := Check(sales, []Item{{SKU: "OLD", OnHand: 4, Cost: 250, HasCost: true}, {SKU: "A", OnHand: 500}}, s)
	if err != nil {
		t.Fatal(err)
	}
	last := r.Lines[len(r.Lines)-1]
	if last.SKU != "OLD" || last.Status != Dead || last.OrderQty != 0 || r.DeadValue != "10.00" {
		t.Errorf("got %+v, dead %s", last, r.DeadValue)
	}
}

func TestReturnsReduceDemandAndNeverGoNegative(t *testing.T) {
	sales := append(steady("A", 2, 10), Sale{Date: date("2026-01-05"), SKU: "A", Qty: -10})
	s := settings()
	s.WindowDays = 10
	r, _ := Check(sales, []Item{{SKU: "A", OnHand: 100}}, s)
	if got := one(t, r).DailySales; got != 1 {
		t.Errorf("got %v", got)
	}
	onlyReturns := append(steady("B", 0, 10), Sale{Date: date("2026-01-05"), SKU: "B", Qty: -3})
	r, _ = Check(onlyReturns, []Item{{SKU: "B", OnHand: 5}}, s)
	if line := one(t, r); line.Status != Dead || line.DailySales != 0 {
		t.Errorf("got %+v", line)
	}
}

func TestItemLeadTimeAndMOQ(t *testing.T) {
	r, _ := Check(steady("A", 1, 90), []Item{{SKU: "A", OnHand: 19, LeadDays: 20, MOQ: 100, Cost: 150, HasCost: true}}, settings())
	line := one(t, r)
	if line.LeadDays != 20 || line.ReorderPoint != 20 || line.OrderQty != 100 || line.OrderCost != "150.00" || r.OrderCost != "150.00" {
		t.Errorf("got %+v", line)
	}
}

func TestSKUMatchingAndMissingCounts(t *testing.T) {
	r, _ := Check(append(steady("abc-1", 1, 30), steady("GHOST", 1, 30)...), []Item{{SKU: "ABC-1", OnHand: 5}}, settings())
	if one(t, r).DailySales != 1 || !slices.Equal(r.NoStockCount, []string{"GHOST"}) {
		t.Errorf("got %+v", r)
	}
}

func TestWindowIsCalendarDaysIncludingZeros(t *testing.T) {
	sales := []Sale{{Date: date("2026-01-01"), SKU: "A", Qty: 90}, {Date: date("2026-03-31"), SKU: "A", Qty: 90}}
	r, _ := Check(sales, []Item{{SKU: "A", OnHand: 1000}}, settings())
	if got := one(t, r).DailySales; got != 2 {
		t.Errorf("got %v", got)
	}
}

func TestBadSettings(t *testing.T) {
	bad := []Settings{{LeadDays: 1, CoverDays: 1, WindowDays: 1, Service: 80}, {LeadDays: 0, CoverDays: 1, WindowDays: 1, Service: 95}, {LeadDays: 1, CoverDays: -1, WindowDays: 1, Service: 95}, {LeadDays: 1, CoverDays: 1, WindowDays: 0, Service: 95}}
	for _, s := range bad {
		if _, err := Check(steady("A", 1, 5), nil, s); err == nil {
			t.Errorf("%+v should fail", s)
		}
	}
	if _, err := Check(nil, nil, Defaults()); err == nil {
		t.Error("no sales should fail")
	}
}

func TestInvariantsOnRandomData(t *testing.T) {
	random := rand.New(rand.NewSource(3))
	for round := 0; round < 400; round++ {
		var sales []Sale
		for i := 0; i < 1+random.Intn(200); i++ {
			sales = append(sales, Sale{Date: date("2026-01-01").AddDate(0, 0, random.Intn(200)), SKU: fmt.Sprintf("S%d", random.Intn(5)), Qty: float64(random.Intn(9) - 1)})
		}
		var items []Item
		for i := 0; i < 5; i++ {
			items = append(items, Item{SKU: fmt.Sprintf("S%d", i), OnHand: float64(random.Intn(500)), LeadDays: float64(random.Intn(30))})
		}
		r, err := Check(sales, items, Defaults())
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Lines) != len(items) {
			t.Fatalf("lost a SKU: %+v", r)
		}
		for _, line := range r.Lines {
			if line.OrderQty < 0 || line.SafetyStock < 0 || line.ReorderPoint < 0 || math.IsNaN(line.DailySales) || line.DailySales < 0 {
				t.Fatalf("bad line %+v", line)
			}
			if (line.Status == OK || line.Status == Over || line.Status == Dead) && line.OrderQty != 0 {
				t.Fatalf("order without need %+v", line)
			}
		}
	}
}

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file.csv")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadSales(t *testing.T) {
	sales, err := LoadSales(write(t, "Report,,\nData;Produto;Quantidade\n25/02/2026;A;1,5\nTotal;;\n01/03/2026;B;2\n"), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sales.Rows) != 2 || sales.Rows[0].Qty != 1.5 || sales.DateFormat != "dmy" || !slices.Equal(sales.Skipped, []int{4}) {
		t.Errorf("got %+v", sales)
	}
	ambiguous := write(t, "Date,SKU,Qty\n03/02/2026,A,1\n04/02/2026,A,1\n")
	if _, err := LoadSales(ambiguous, nil, ""); err == nil || !strings.Contains(err.Error(), "dmy or mdy") {
		t.Errorf("got %v", err)
	}
	if sales, err := LoadSales(ambiguous, nil, "mdy"); err != nil || sales.Rows[0].Date.Format("2006-01-02") != "2026-03-02" {
		t.Errorf("%v %+v", err, sales)
	}
}

func TestLoadStock(t *testing.T) {
	stock, err := LoadStock(write(t, "SKU,On Hand,Lead Time,Cost\nA,10,7,2.50\na,5,,\nB,-3,,\n,4,,\nC,oops,,\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(stock.Items) != 2 || stock.Items[0].OnHand != 15 || stock.Items[0].LeadDays != 7 || stock.Items[0].Cost != 250 || stock.Items[1].OnHand != 0 {
		t.Errorf("got %+v", stock.Items)
	}
	if !slices.Equal(stock.Skipped, []int{5, 6}) || len(stock.Warnings) != 2 {
		t.Errorf("got %+v", stock)
	}
}

func TestLoadErrors(t *testing.T) {
	unknown := write(t, "Foo,Bar\n1,2\n")
	if _, err := LoadSales(unknown, nil, ""); err == nil || !strings.Contains(err.Error(), "First row: Foo, Bar") {
		t.Errorf("got %v", err)
	}
	if _, err := LoadStock(unknown, map[string]string{"sku": "Nope"}); err == nil || !strings.Contains(err.Error(), "Columns found: Foo, Bar") {
		t.Errorf("got %v", err)
	}
	if stock, err := LoadStock(unknown, map[string]string{"sku": "Foo", "qty": "Bar"}); err != nil || len(stock.Items) != 1 {
		t.Errorf("%v %+v", err, stock)
	}
	if _, err := LoadSales("/no/such.csv", nil, ""); err == nil {
		t.Error("missing file should fail")
	}
	if _, err := LoadStock(t.TempDir(), nil); err == nil {
		t.Error("folder should fail")
	}
}
