# runwaystock: inventory reorder points from your sales and stock files

[![npm](https://img.shields.io/npm/v/runwaystock)](https://www.npmjs.com/package/runwaystock) [![CI](https://github.com/GillMacedo/runwaystock/actions/workflows/ci.yml/badge.svg)](https://github.com/GillMacedo/runwaystock/actions/workflows/ci.yml) [![MIT license](https://img.shields.io/badge/license-MIT-blue)](LICENSE) [![MCP Registry](https://img.shields.io/badge/MCP_Registry-listed-green)](https://registry.modelcontextprotocol.io/v0/servers?search=io.github.GillMacedo/runwaystock)

**Know what to reorder, how much to buy, when each product runs out, and how much money is stuck in dead stock. For e-commerce stores, retailers and small businesses that plan inventory in spreadsheets.**

Inventory planning software is expensive, and spreadsheet formulas break. `runwaystock` reads a sales export and a stock count as CSV files, and applies the standard reorder point and safety stock formulas to every SKU. It's an MCP server and a single Go binary, with no account, no API key and no network access.

## What you can ask your agent

- "What do I need to reorder this week, and how much?"
- "Which products will run out first?"
- "How much money is tied up in dead stock?"
- "Which products am I overstocked on?"

## Install

As a Claude Code plugin, which adds the MCP server and a skill that tells Claude when to use it. You need Node, which runs the server through `npx`:

    claude plugin marketplace add GillMacedo/runwaystock
    claude plugin install runwaystock@runwaystock-marketplace

For other MCP clients:

```json
{
  "mcpServers": {
    "runwaystock": {
      "command": "npx",
      "args": ["-y", "runwaystock", "serve"]
    }
  }
}
```

Or build the binary with Go 1.27 or newer:

    go install github.com/GillMacedo/runwaystock@latest

## Usage

Without installing anything, run `npx runwaystock check sales.csv stock.csv`. This example uses the made-up files in `examples/`:

    $ go run . check examples/sales.csv examples/stock.csv
    As of 2026-09-30, using 90 days of sales
    ORDER NOW        WIDGET-RED: buy 93  costs 418.50  (40 on hand, 2.83 a day, out by 2026-10-14, reorder at 48)
    OK               MUG-LARGE  (70 on hand, 35 days left)
    OK               CAP-DEMO  (300 on hand, 51 days left)
    OVERSTOCKED      WIDGET-BLUE  (120 on hand, 277 days left)
    OVERSTOCKED      BAG-CANVAS  (500 on hand, 3462 days left)
    NO RECENT SALES  POSTER-OLD  (60 on hand, worth 120.00)
    NO RECENT SALES  TSHIRT-UNSOLD  (35 on hand, worth 420.00)
    NOT IN STOCK FILE  STICKER-NEW sold recently
    SKIPPED sales row 328
    1 to order now, costing 418.50. 540.00 tied up in stock with no recent sales.

Set your own assumptions with `--lead 21`, `--cover 45`, `--window 60` and `--service 99`. A lead time in the stock file overrides `--lead` for that SKU. Add `--json` for machine-readable output.

## How it calculates

    safety stock  = z x standard deviation of daily sales x square root of lead days
    reorder point = daily sales x lead days + safety stock
    order qty     = daily sales x (lead days + cover days) + safety stock - on hand

Daily sales are averaged over calendar days, including days with no sales. Returns reduce demand. Orders round up to any minimum order quantity (MOQ) in the stock file. Service levels of 90, 95 and 99 use z values of 1.28, 1.65 and 2.33.

| Status | Meaning |
|---|---|
| **Order now** | On hand is at or below the reorder point |
| **Order soon** | It will reach the reorder point within 7 days |
| **Ok** | Enough stock for now |
| **Overstocked** | More than 180 days of stock left |
| **No recent sales** | No net sales in the window |

## MCP tools

| Tool | What it does |
|---|---|
| `inventory_check` | Returns each SKU's status and order, most urgent first |
| `inventory_inspect_csv` | Shows detected columns and row counts, without row contents |

Both are read-only, and neither can place orders. `inventory_check` returns SKUs and quantities to the MCP client, so they reach whichever AI model you use. The CLI keeps everything on your machine.

## FAQ

**How do I calculate a reorder point?** Use the formula above, or run `npx runwaystock check sales.csv stock.csv` to do it for every SKU.

**Does it work with Shopify, Amazon or WooCommerce?** It reads CSV and recognizes common column names such as `Lineitem sku` and `Lineitem quantity`, and you can name others with `--sales-columns`. It doesn't connect to those platforms, and it hasn't been tested on each one's real exports yet.

**How much sales history do I need?** At least 28 days, or it warns that figures are rough. It uses the last 90 days by default.

## Limits

- It assumes recent sales predict the near future, so it doesn't see seasonality, promotions or growth.
- Sales lost while out of stock look like low demand, which understates what you'd need.
- The safety stock formula assumes fairly regular demand and a fixed lead time, so treat results as a starting point and check before buying.
- It reads CSV only, up to 50 MB, and doesn't handle bundles or unit conversions.
- So far it has been tested on made-up data, not real exports. If yours fails, please open an issue with the column headers, not your data.

## Contributing

Contributions are welcome, from people and from people working with AI agents. See [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues:

- Add column names from more e-commerce platforms and languages
- Add `--out` to write a purchase list to a CSV
- Group the order list by supplier
- Add a seasonality option that compares with the same period last year
- Add a plugin icon

### How pull requests work

1. **Open a PR.** Fork the repo, work on a branch, and keep it to one change. Write a title that says what changes for users, because it becomes the changelog line: `feat:` goes under Added and `fix:` under Fixed.
2. **Automatic check.** Code changes need tests, and `make test` must pass. Changes over 30 lines need a linked open issue (`Closes #<number>`), and a PR can have at most 800 changed lines. If something is missing, a comment tells you what to fix.
3. **Review.** Claude reviews each PR automatically. Code PRs also get a security review and are tested in a sandbox. The review then merges the PR or asks for changes. PRs that change CI, agent settings, dependencies or build files go to the maintainer, as does anything the security review doesn't clear.
4. **Release.** A merge that changes what users install becomes a new version within about 20 minutes, with a GitHub Release and, if the project has one, its npm package. Docs and CI changes wait for the next release.

## AI disclosure

This project was largely generated with Claude Code and reviewed by its maintainer.

## About me

I'm Giulliano Macedo, and I build developer tools at GoTech Solutions. Follow along or get in touch:

- GitHub: [GillMacedo](https://github.com/GillMacedo)
- LinkedIn: [giulliano](https://www.linkedin.com/in/giulliano/)
- X: [@giumacedo](https://x.com/giumacedo)

If `runwaystock` saved you from a stockout, a star helps others find it.

---

MIT licensed.

Built by [Giulliano Macedo](https://github.com/GillMacedo), GoTech Solutions · [LinkedIn](https://www.linkedin.com/in/giulliano/) · [X](https://x.com/giumacedo)
