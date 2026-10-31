---
name: checking-stock
description: Works out what to reorder and how much from a sales CSV and a stock CSV, with reorder points, safety stock, stockout dates and dead stock. Use when the user asks about inventory planning, purchase orders, running out of stock, overstock or slow-moving products.
---

# Checking stock

Call `inventory_check` instead of calculating by hand, because the formulas need daily figures across every SKU. If it can't find columns, the error says how to name them, and `inventory_inspect_csv` shows what it detected. If dates are ambiguous, ask whether they're day-first or month-first.

Ask for the supplier lead time if it matters and isn't in the stock file. The default of 14 days is only an assumption, so say when it was used.

## Reporting results
- Lead with what to order now, the quantities and the cost.
- Then give items to order soon, overstock and dead stock value.
- Report `sold_but_not_in_stock_file`, `warnings` and skipped rows.

Results are estimates from recent sales, and they don't include seasonality or promotions, so ask the user to check before buying. Don't place orders without approval.
