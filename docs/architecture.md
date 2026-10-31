# Architecture

    sales.csv ─> load ─┐
                       ├─> check ─> CLI and MCP tools
    stock.csv ─> load ─┘

**Loading:** finds the header in the first 20 rows. Unreadable rows are reported. Stock rows repeating a SKU are summed with a warning, since they're usually several locations. SKUs match without regard to case.

**Window:** the last 90 days up to the newest sale, or the whole history if shorter. Daily sales are built per calendar day, with zeros, so quiet days count.

**Formulas:** see the README. Results round up, because a fraction of a unit can't be bought.

**Statuses:** decided from unrounded values, then sorted most urgent first, with the soonest stockout first within each status.

**Packaging:** one npm package holds all five binaries, and `npm/bin.js` picks the right one.

See `docs/decisions/`.
