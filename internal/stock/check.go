package stock

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Statuses, from most to least urgent.
const (
	OrderNow = "order now"
	Soon     = "order soon"
	OK       = "ok"
	Over     = "overstocked"
	Dead     = "no recent sales"
)

const (
	soonDays      = 7
	overstockDays = 180
	shortHistory  = 28
)

var zScores = map[int]float64{90: 1.28, 95: 1.65, 99: 2.33}

// Settings are the planning assumptions. Stock file lead times override LeadDays.
type Settings struct {
	LeadDays   float64
	CoverDays  float64
	WindowDays int
	Service    int
}

// Defaults are reasonable starting assumptions for a small business.
func Defaults() Settings {
	return Settings{LeadDays: 14, CoverDays: 30, WindowDays: 90, Service: 95}
}

// Line is the result for one SKU.
type Line struct {
	SKU          string  `json:"sku"`
	Status       string  `json:"status"`
	OnHand       float64 `json:"on_hand"`
	DailySales   float64 `json:"daily_sales"`
	DaysLeft     float64 `json:"days_left,omitempty"`
	StockoutDate string  `json:"stockout_date,omitempty"`
	LeadDays     float64 `json:"lead_days"`
	SafetyStock  float64 `json:"safety_stock"`
	ReorderPoint float64 `json:"reorder_point"`
	OrderQty     float64 `json:"order_qty"`
	OrderCost    string  `json:"order_cost,omitempty"`
	StockValue   string  `json:"stock_value,omitempty"`
	sortDays     float64
}

// Result covers every SKU in the stock file.
type Result struct {
	AsOf         string   `json:"as_of"`
	WindowDays   int      `json:"window_days"`
	Lines        []Line   `json:"lines"`
	OrderNow     int      `json:"order_now"`
	DeadValue    string   `json:"dead_stock_value"`
	OrderCost    string   `json:"order_now_cost"`
	NoStockCount []string `json:"sold_but_not_in_stock_file"`
	Warnings     []string `json:"warnings"`
}

func formatCents(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

func round(value float64, places int) float64 {
	scale := math.Pow(10, float64(places))
	return math.Round(value*scale) / scale
}

// sampleStdDev is the standard deviation of daily sales, zeros included.
func sampleStdDev(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += (value - mean) * (value - mean)
	}
	return math.Sqrt(sum / float64(len(values)-1))
}

// Check applies the standard reorder point formula to each SKU:
//
//	safety stock  = z x daily standard deviation x sqrt(lead days)
//	reorder point = daily sales x lead days + safety stock
//	order qty     = daily sales x (lead days + cover days) + safety stock - on hand
func Check(sales []Sale, items []Item, settings Settings) (Result, error) {
	z, ok := zScores[settings.Service]
	if !ok {
		return Result{}, fmt.Errorf("service level must be 90, 95 or 99, not %d", settings.Service)
	}
	if settings.WindowDays < 1 || settings.LeadDays <= 0 || settings.CoverDays < 0 {
		return Result{}, fmt.Errorf("window and lead days must be above 0, and cover days can't be negative")
	}
	result := Result{Lines: []Line{}, NoStockCount: []string{}, Warnings: []string{}, DeadValue: "0.00", OrderCost: "0.00"}

	var first, end time.Time
	for _, sale := range sales {
		if end.IsZero() || sale.Date.After(end) {
			end = sale.Date
		}
		if first.IsZero() || sale.Date.Before(first) {
			first = sale.Date
		}
	}
	if end.IsZero() {
		return result, fmt.Errorf("the sales file has no readable sales")
	}
	window := settings.WindowDays
	history := int(end.Sub(first).Hours()/24) + 1
	if history < window {
		window = history
	}
	if window < shortHistory {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Only %d days of sales history, so these figures are rough.", window))
	}
	start := end.AddDate(0, 0, -(window - 1))
	result.AsOf = end.Format("2006-01-02")
	result.WindowDays = window

	daily := map[string][]float64{}
	names := map[string]string{}
	for _, sale := range sales {
		key := skuKey(sale.SKU)
		if _, ok := names[key]; !ok {
			names[key] = sale.SKU
		}
		if sale.Date.Before(start) {
			continue
		}
		if daily[key] == nil {
			daily[key] = make([]float64, window)
		}
		daily[key][int(sale.Date.Sub(start).Hours()/24)] += sale.Qty
	}

	known := map[string]bool{}
	var orderCost, deadValue int64
	for _, item := range items {
		key := skuKey(item.SKU)
		known[key] = true
		days := daily[key]
		if days == nil {
			days = make([]float64, window)
		}
		total := 0.0
		for _, value := range days {
			total += value
		}
		mean := total / float64(window)
		if mean < 0 {
			mean = 0
		}
		lead := item.LeadDays
		if lead == 0 {
			lead = settings.LeadDays
		}
		safety := z * sampleStdDev(days, mean) * math.Sqrt(lead)
		reorder := mean*lead + safety
		line := Line{SKU: item.SKU, OnHand: item.OnHand, DailySales: round(mean, 2), LeadDays: lead, SafetyStock: math.Ceil(safety - 1e-9), ReorderPoint: math.Ceil(reorder - 1e-9), sortDays: math.Inf(1)}
		if item.HasCost {
			line.StockValue = formatCents(int64(math.Round(item.OnHand * float64(item.Cost))))
		}
		if mean == 0 {
			line.Status = Dead
			if item.HasCost {
				deadValue += int64(math.Round(item.OnHand * float64(item.Cost)))
			}
			result.Lines = append(result.Lines, line)
			continue
		}
		daysLeft := item.OnHand / mean
		line.sortDays = daysLeft
		line.DaysLeft = round(daysLeft, 1)
		line.StockoutDate = end.AddDate(0, 0, int(daysLeft)).Format("2006-01-02")
		switch {
		case item.OnHand <= reorder:
			line.Status = OrderNow
		case item.OnHand <= reorder+mean*soonDays:
			line.Status = Soon
		case daysLeft > overstockDays:
			line.Status = Over
		default:
			line.Status = OK
		}
		if line.Status == OrderNow || line.Status == Soon {
			qty := math.Ceil(mean*(lead+settings.CoverDays) + safety - item.OnHand - 1e-9)
			if qty < 0 {
				qty = 0
			}
			if item.MOQ > 0 && qty > 0 && qty < item.MOQ {
				qty = item.MOQ
			}
			line.OrderQty = qty
			if item.HasCost && qty > 0 {
				cost := int64(math.Round(qty * float64(item.Cost)))
				line.OrderCost = formatCents(cost)
				if line.Status == OrderNow {
					orderCost += cost
				}
			}
		}
		if line.Status == OrderNow {
			result.OrderNow++
		}
		result.Lines = append(result.Lines, line)
	}

	rank := map[string]int{OrderNow: 0, Soon: 1, OK: 2, Over: 3, Dead: 4}
	sort.SliceStable(result.Lines, func(i, j int) bool {
		a, b := result.Lines[i], result.Lines[j]
		if rank[a.Status] != rank[b.Status] {
			return rank[a.Status] < rank[b.Status]
		}
		if a.sortDays != b.sortDays {
			return a.sortDays < b.sortDays
		}
		return a.SKU < b.SKU
	})
	for key := range daily {
		if !known[key] {
			result.NoStockCount = append(result.NoStockCount, names[key])
		}
	}
	sort.Strings(result.NoStockCount)
	result.DeadValue = formatCents(deadValue)
	result.OrderCost = formatCents(orderCost)
	return result, nil
}
