# Mallow

A Go-native, lightweight implementation of the [Malloy](https://github.com/malloydata/malloy) data language.

`Mallow` enables embedding Mallow's powerful relational modeling and querying capabilities directly into your Go applications. It translates Mallow syntax into optimized, dialect-specific SQL (Postgres, DuckDB) and offers simple integration with Go's `database/sql`.

## Key Features

- **Native Go API**: Idiomatic interfaces built on top of `database/sql`.
- **Multi-Dialect Support**: Native compilation for **Postgres** and **DuckDB**, easily extensible.
- **Two-Phase Compilation Architecture**: 
  - **Translator (Phase 1):** Semantic analysis using a symbol table (`FieldSpace`), generating a serializable **Intermediate Representation (IR)**. Powered by [Participle](https://github.com/alecthomas/participle).
  - **Compiler (Phase 2):** Generates optimized, dialect-specific SQL from the IR.
- **Relational Pipelines:** Translates Mallow's multi-stage `->` pipeline operations into SQL Common Table Expressions (CTEs).
- **Automated Joins:** Implicit SQL generation for `join_one`, `join_many`, and `join`.
- **Nested Queries:** Experimental parsing and translation for Mallow nests.
- **Dynamic Results:** Read structured datasets seamlessly into JSON or Go structs.

## Installation

```bash
go get github.com/galihanggara68/mallow
```

## Quick Start

### 1. Execute a Query using DuckDB (CSV)

Mallow shines at analyzing local datasets. You can query a CSV using DuckDB directly.

```go
package main

import (
	"context"
	"database/sql"
	"log"

	"github.com/galihanggara68/mallow"
	"github.com/galihanggara68/mallow/pkg/compiler"

	_ "github.com/duckdb/duckdb-go/v2"
)

func main() {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	engine := mallow.New(&compiler.DuckDBDialect{}, db)

	// Define your Mallow source and query
	session := engine.FromString(`
		source: flights is table('read_csv_auto("flights.csv", ignore_errors=true)') {
			dimension: carrier
			measure: flight_count is count()
		}

		query: top_carriers is flights -> {
			group_by: carrier
			aggregate: flight_count
		}
	`)

	// Generate SQL
	sqlQuery, err := session.GetSQL("top_carriers")
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Generated SQL: %s", sqlQuery)

	// Run Query
	rows, err := session.Run(context.Background(), "top_carriers")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	// Extract Results
	for rows.Next() {
		var carrier string
		var count int
		if err := rows.Scan(&carrier, &count); err != nil {
			log.Fatal(err)
		}
		log.Printf("Carrier: %s, Flights: %d", carrier, count)
	}
}
```

### 2. Multi-Stage Pipelines

Mallow's core strength is its multi-stage pipeline capability (`->`), which `mallow` dynamically compiles to SQL `WITH` CTEs.

```go
session := engine.FromString(`
	source: orders is table('public.orders') {
		dimension: brand
		measure: total_sales is sum(amount)
	}
	
	query: top_brands is orders -> {
		group_by: brand
		aggregate: total_sales
	} -> {
		project: brand
		where: total_sales > 1000
	}
`)

rows, _ := session.Run(context.Background(), "top_brands")
// The engine automatically builds the multi-stage SQL execution 
// and returns only brands with total_sales > 1000.
```

### 3. Loading from Files

Instead of inline strings, you can load models from `.mallow` files directly:

```go
session := engine.FromFile("models/sales.mallow")
sqlStr, err := session.GetSQL("revenue_report")
```

## Supported Syntax & Expressions

- **Declarations**: `source`, `query`, `run`
- **Data Sources**: `table()`, inheritance from other sources.
- **Fields**: `dimension`, `measure`
- **Joins**: `join_one`, `join_many`, `on`
- **Pipelines**: `->`
- **Query Operations**: `project`, `aggregate`, `group_by`, `nest`, `where`
- **Functions**: Built-in support for `count()`, `sum()`, `avg()`, `day()` (fully extensible per-dialect).

## Architecture

1.  **Translator** (`pkg/translator`): Parses text into an AST using `alecthomas/participle`. It performs semantic resolution via a `FieldSpace` symbol table and maps the output to Mallow IR (`pkg/ir`).
2.  **IR** (`pkg/ir`): Strongly-typed, serializable intermediate representation defining sources, queries, stages, and their underlying expressions.
3.  **Compiler** (`pkg/compiler`): Consumes IR and a target `Dialect` to recursively generate pure standard SQL, preserving multi-stage logic via CTE chaining.

## Extending the Compiler

To add a new SQL Dialect, implement the `compiler.Dialect` interface:

```go
type Dialect interface {
	QuoteIdentifier(id string) string
	DatePart(part string, expr string) string
}
```

Add dialect-specific SQL translation for functions by registering a custom blueprint:

```go
compiler.RegisterFunction("month", func(dialect compiler.Dialect, args []string) (string, error) {
    return dialect.DatePart("MONTH", args[0]), nil
})
```

## Roadmap

- [ ] **Turducken SQL:** Nested query results via JSON aggregation.
- [ ] **Advanced Expressions:** Full support for calculated fields and complex filters.
- [ ] **Schema Introspection:** Automatically discover dimensions from database metadata.
- [ ] **CLI Tool:** A standalone binary for executing Mallow files.

## Development

A `Makefile` is provided to simplify common development tasks.

- **Run tests**: `make test`
- **Run linter**: `make lint`
- **Check formatting**: `make fmt-check`
- **Fix formatting**: `make fmt`
- **Build binary**: `make build`
- **Run all checks**: `make all`

## License

MIT
