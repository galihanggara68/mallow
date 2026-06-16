//go:build e2e

package mallow_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	mallow "github.com/galihanggara68/mallow"
	"github.com/galihanggara68/mallow/pkg/compiler"
	_ "github.com/lib/pq"
)

func TestEngineE2EPipeline(t *testing.T) {
	dbURL := os.Getenv("MALLOW_POSTGRES_URL")
	if dbURL == "" {
		t.Skip("MALLOW_POSTGRES_URL not set, skipping e2e pipeline test")
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("skipping e2e pipeline test, database not available: %v", err)
	}

	engine := mallow.New(&compiler.PostgresDialect{}, db)

	// Pipeline: Group by bank -> Then project
	sourceText := `
		source: cc is table('datamart.cc_records') {
			dimension: issuing_bank
			measure: card_count is count()
		}

		query: top_banks is cc -> {
			group_by: issuing_bank
			aggregate: card_count
		} -> {
			project: issuing_bank
		}
	`
	session := engine.FromString(sourceText)

	rows, err := session.Run(context.Background(), "top_banks")
	if err != nil {
		t.Fatalf("failed to run query: %v", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var bank sql.NullString
		if err := rows.Scan(&bank); err != nil {
			t.Fatalf("failed to scan row: %v", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}

	if count == 0 {
		t.Logf("Query ran successfully, but returned 0 rows.")
	}
}

func TestEngineE2EJoinsAndTurducken(t *testing.T) {
	dbURL := os.Getenv("MALLOW_POSTGRES_URL")
	if dbURL == "" {
		t.Skip("MALLOW_POSTGRES_URL not set, skipping e2e join/turducken test")
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("skipping e2e join/turducken test, database not available: %v", err)
	}

	engine := mallow.New(&compiler.PostgresDialect{}, db)

	sourceText := `
		source: order_items is table('datamart.order_items') {
			dimension: order_id, product_name, quantity, unit_price
			measure: total_quantity is sum(quantity)
		}

		source: orders is table('datamart.orders') {
			dimension: order_id, customer_id, status
			measure: order_count is count()
			join_many: items is order_items on order_id = items.order_id
		}

		source: customers is table('datamart.customers') {
			dimension: customer_id, name, region
			join_many: orders is orders on customer_id = orders.customer_id
		}

		query: join_test is orders -> {
			project: order_id, product_name is items.product_name, quantity is items.quantity
		}

		query: turducken_test is customers -> {
			group_by: customer_id, name
			nest: orders_report is orders -> {
				group_by: order_id, status
				aggregate: order_count
				nest: items_report is items -> {
					group_by: product_name
					aggregate: total_quantity
				}
			}
		}
	`
	session := engine.FromString(sourceText)

	t.Run("Join Test", func(t *testing.T) {
		sqlStr, err := session.GetSQL("join_test")
		if err != nil {
			t.Fatalf("failed to compile join_test: %v", err)
		}
		t.Logf("Join Test SQL: %s", sqlStr)

		rows, err := session.Run(context.Background(), "join_test")
		if err != nil {
			t.Fatalf("failed to run join_test: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var orderID int
			var product sql.NullString
			var qty int
			if err := rows.Scan(&orderID, &product, &qty); err != nil {
				t.Fatalf("failed to scan join_test row: %v", err)
			}
			count++
		}
		if count == 0 {
			t.Errorf("expected rows in join_test, got 0")
		}
	})

	t.Run("Turducken Test", func(t *testing.T) {
		sqlStr, err := session.GetSQL("turducken_test")
		if err != nil {
			t.Fatalf("failed to compile turducken_test: %v", err)
		}
		t.Logf("Turducken Test SQL: %s", sqlStr)

		rows, err := session.Run(context.Background(), "turducken_test")
		if err != nil {
			t.Fatalf("failed to run turducken_test: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var id int
			var name string
			var ordersReportRaw any
			if err := rows.Scan(&id, &name, &ordersReportRaw); err != nil {
				t.Fatalf("failed to scan turducken_test row: %v", err)
			}
			count++
			if ordersReportRaw == nil {
				t.Errorf("expected orders_report to be non-nil for %s", name)
			}
		}
		if count == 0 {
			t.Errorf("expected rows in turducken_test, got 0")
		}
	})
}

func TestEngineE2E(t *testing.T) {
	dbURL := os.Getenv("MALLOW_POSTGRES_URL")
	if dbURL == "" {
		t.Skip("MALLOW_POSTGRES_URL not set, skipping e2e test")
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	err = db.Ping()
	if err != nil {
		t.Skipf("skipping e2e test, database not available: %v", err)
	}

	engine := mallow.New(&compiler.PostgresDialect{}, db)

	// A basic Mallow string querying datamart.cc_records
	sourceText := `
		source: cc is table('datamart.cc_records') {
			dimension: issuing_bank
			measure: card_count is count()
		}

		query: bank_stats is cc -> {
			group_by: issuing_bank
			aggregate: card_count
		}
	`
	session := engine.FromString(sourceText)

	// Test compilation
	sqlStr, err := session.GetSQL("bank_stats")
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}

	expectedSQL := "SELECT \"issuing_bank\", COUNT(*) AS \"card_count\" FROM \"datamart\".\"cc_records\" GROUP BY 1"
	if sqlStr != expectedSQL {
		t.Logf("Compiled SQL: %s", sqlStr)
	}

	// Test execution
	rows, err := session.Run(context.Background(), "bank_stats")
	if err != nil {
		t.Fatalf("failed to run query: %v", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var bank sql.NullString
		var cardCount int
		if err := rows.Scan(&bank, &cardCount); err != nil {
			t.Fatalf("failed to scan row: %v", err)
		}
		t.Logf("Bank: %v, Count: %d", bank.String, cardCount)
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}

	if count == 0 {
		t.Logf("Query ran successfully, but returned 0 rows. (table might be empty)")
	}
}

func TestEngineE2EComplex(t *testing.T) {
	dbURL := os.Getenv("MALLOW_POSTGRES_URL")
	if dbURL == "" {
		t.Skip("MALLOW_POSTGRES_URL not set, skipping e2e complex test")
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("skipping e2e complex test, database not available: %v", err)
	}

	engine := mallow.New(&compiler.PostgresDialect{}, db)

	sourceText := `
		source: cc is table('datamart.cc_records') {
			dimension: issuing_bank
		}

		query: bank_report is cc -> {
			group_by: issuing_bank, branch is 'HQ'
			aggregate: card_count is count(), total_limit is sum(credit_limit)
		}
	`
	session := engine.FromString(sourceText)

	sqlStr, err := session.GetSQL("bank_report")
	if err != nil {
		t.Fatalf("failed to compile: %v", err)
	}
	t.Logf("SQL: %s", sqlStr)

	rows, err := session.Run(context.Background(), "bank_report")
	if err != nil {
		t.Fatalf("failed to run query: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var bank, branch sql.NullString
		var count, limit int
		if err := rows.Scan(&bank, &branch, &count, &limit); err != nil {
			t.Fatalf("failed to scan row: %v", err)
		}
	}
}

func TestEngineE2EPostgresThreeTables(t *testing.T) {
	dbURL := os.Getenv("MALLOW_POSTGRES_URL")
	if dbURL == "" {
		t.Skip("MALLOW_POSTGRES_URL not set, skipping e2e postgres three tables test")
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("skipping e2e postgres three tables test, database not available: %v", err)
	}

	engine := mallow.New(&compiler.PostgresDialect{}, db)

	sourceText := `
		source: order_items is table('datamart.order_items') {
			dimension: order_id, product_name, quantity, unit_price
			measure: total_quantity is sum(quantity)
			measure: total_revenue is sum(quantity * unit_price)
		}

		source: orders is table('datamart.orders') {
			dimension: order_id, customer_id, status
			measure: order_count is count()
			join_many: items is order_items on order_id = items.order_id
		}

		source: customers is table('datamart.customers') {
			dimension: customer_id, name, region
			join_many: orders is orders on customer_id = orders.customer_id
		}

		query: customer_report is customers -> {
			group_by: region, name
			aggregate: order_count is count(orders.order_id)
			nest: orders_list is orders -> {
				group_by: order_id, status
				aggregate: items_count is sum(items.quantity), revenue is sum(items.quantity * items.unit_price)
				nest: items_list is items -> {
					group_by: product_name
					aggregate: total_quantity is sum(quantity), total_revenue is sum(quantity * unit_price)
				}
			}
		}
	`
	session := engine.FromString(sourceText)

	t.Run("Compile and Run Customer Report", func(t *testing.T) {
		sqlStr, err := session.GetSQL("customer_report")
		if err != nil {
			t.Fatalf("failed to compile customer_report: %v", err)
		}
		t.Logf("SQL: %s", sqlStr)

		rows, err := session.Run(context.Background(), "customer_report")
		if err != nil {
			t.Fatalf("failed to run customer_report: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var region, name string
			var orderCount int
			var ordersListRaw any
			if err := rows.Scan(&region, &name, &orderCount, &ordersListRaw); err != nil {
				t.Fatalf("failed to scan row: %v", err)
			}
			count++
			if ordersListRaw == nil {
				t.Errorf("expected orders_list to be non-nil for %s", name)
			}
		}
		if count == 0 {
			t.Errorf("expected rows in customer_report, got 0")
		}
		t.Logf("Successfully processed %d customers", count)
	})
}

func TestDuckDBIntegration(t *testing.T) {
	ccPath := os.Getenv("MALLOW_CC_PATH")
	recordsPath := os.Getenv("MALLOW_RECORDS_PATH")

	if ccPath == "" || recordsPath == "" {
		t.Skip("MALLOW_CC_PATH or MALLOW_RECORDS_PATH not set, skipping duckdb integration test")
	}

	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("failed to open duckdb: %v", err)
	}
	defer db.Close()

	engine := mallow.New(&compiler.DuckDBDialect{}, db)

	t.Run("Simple Aggregation", func(t *testing.T) {
		sourceText := `
			source: cc is table('read_csv_auto("` + ccPath + `", ignore_errors=true)') {
				dimension: issuing_bank
				measure: card_count is count()
			}

			query: top_banks is cc -> {
				group_by: issuing_bank
				aggregate: card_count
			}
		`
		session := engine.FromString(sourceText)
		rows, err := session.Run(context.Background(), "top_banks")
		if err != nil {
			t.Fatalf("failed to run query: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var bank sql.NullString
			var cardCount int
			if err := rows.Scan(&bank, &cardCount); err != nil {
				t.Fatalf("failed to scan row: %v", err)
			}
			count++
		}
		if count == 0 {
			t.Errorf("expected some rows, got 0")
		}
	})

	t.Run("Multi-stage Pipeline", func(t *testing.T) {
		sourceText := `
			source: cc is table('read_csv_auto("` + ccPath + `", ignore_errors=true)') {
				dimension: issuing_bank
				measure: card_count is count()
			}

			query: top_banks_projected is cc -> {
				group_by: issuing_bank
				aggregate: card_count
			} -> {
				project: issuing_bank, card_count
			}
		`
		session := engine.FromString(sourceText)
		rows, err := session.Run(context.Background(), "top_banks_projected")
		if err != nil {
			t.Fatalf("failed to run query: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var bank sql.NullString
			var cardCount int
			if err := rows.Scan(&bank, &cardCount); err != nil {
				t.Fatalf("failed to scan row: %v", err)
			}
			count++
		}
		if count == 0 {
			t.Errorf("expected some rows, got 0")
		}
	})

	t.Run("Complex Pipeline with Quoted Identifiers", func(t *testing.T) {
		// 100 Records.csv has "Emp ID", "First Name", "Salary"
		sourceText := `
			source: emps is table('read_csv_auto("` + recordsPath + `", ignore_errors=true)') {
				dimension: emp_id is ` + "`Emp ID`" + `
				dimension: first_name is ` + "`First Name`" + `
				dimension: last_name is ` + "`Last Name`" + `
				measure: total_salary is sum(Salary)
				measure: avg_salary is total_salary / count()
			}

			query: salary_report is emps -> {
				group_by: first_name
				aggregate: total_salary, avg_salary
			} -> {
				project: first_name, total_salary
				where: total_salary > 100000
			}
		`
		session := engine.FromString(sourceText)
		rows, err := session.Run(context.Background(), "salary_report")
		if err != nil {
			t.Fatalf("failed to run query: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var name string
			var salary float64
			if err := rows.Scan(&name, &salary); err != nil {
				t.Fatalf("failed to scan row: %v", err)
			}
			count++
		}
		t.Logf("Found %d high salary employees", count)
	})

	t.Run("Turducken (Nested Query)", func(t *testing.T) {
		sourceText := `
			source: cc is table('read_csv_auto("` + ccPath + `", ignore_errors=true)') {
				dimension: issuing_bank
				measure: card_count is count()
			}

			query: banks_with_details is cc -> {
				group_by: issuing_bank
				aggregate: card_count
				nest: details is {
					group_by: issuing_bank
					aggregate: card_count
				}
			}
		`
		session := engine.FromString(sourceText)
		rows, err := session.Run(context.Background(), "banks_with_details")
		if err != nil {
			t.Fatalf("failed to run query: %v", err)
		}
		defer rows.Close()

		for rows.Next() {
			var bank string
			var count int
			var detailsRaw any
			if err := rows.Scan(&bank, &count, &detailsRaw); err != nil {
				t.Fatalf("failed to scan row: %v", err)
			}
			if detailsRaw == nil {
				t.Errorf("expected details to be non-empty")
			}
		}
	})
}

func TestEngineE2ETransportationRevenue(t *testing.T) {
	dbURL := os.Getenv("MALLOW_POSTGRES_URL")
	if dbURL == "" {
		t.Skip("MALLOW_POSTGRES_URL not set, skipping e2e transportation revenue test")
	}
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Skipf("skipping e2e transportation revenue test, database not available: %v", err)
	}

	engine := mallow.New(&compiler.PostgresDialect{}, db)

	sourceText := `
		source: trans_customers is table('datamart.trans_customers') {
			dimension: customer_id, name, customer_type
		}
		source: routes_lanes is table('datamart.routes_lanes') {
			dimension: route_id, distance_miles, origin, destination
		}
		source: invoices is table('datamart.invoices') {
			dimension: trip_id, total_amount, status, invoice_date
		}
		source: fuel_surcharges is table('datamart.fuel_surcharges') {
			dimension: trip_id, amount, surcharge_type
		}
		source: trips is table('datamart.trips') {
			dimension: trip_id, customer_id, route_id, trip_date
			join_one: customer is trans_customers on customer_id = customer.customer_id
			join_one: route is routes_lanes on route_id = route.route_id
			join_one: invoice is invoices on trip_id = invoice.trip_id
			join_one: surcharge is fuel_surcharges on trip_id = surcharge.trip_id
			
			measure: total_revenue is sum(invoice.total_amount)
			measure: total_surcharge is sum(surcharge.amount)
			measure: total_miles is sum(route.distance_miles)
			measure: revenue_per_mile is sum(invoice.total_amount) / sum(route.distance_miles)
		}

		query: revenue_report is trips -> {
			group_by: customer_name is customer.name
			aggregate: total_revenue, total_surcharge, total_miles, revenue_per_mile
		}

		query: revenue_by_date is trips -> {
			group_by: trip_date
			aggregate: total_revenue, revenue_per_mile
		}

		query: revenue_by_date_with_routes is trips -> {
			group_by: trip_date
			aggregate: total_revenue, revenue_per_mile
			nest: route_details is {
				group_by: origin is route.origin, destination is route.destination
				aggregate: total_revenue, total_miles, revenue_per_mile
			}
		}
	`
	session := engine.FromString(sourceText)

	t.Run("Compile and Run Revenue Report", func(t *testing.T) {
		sqlStr, err := session.GetSQL("revenue_report")
		if err != nil {
			t.Fatalf("failed to compile revenue_report: %v", err)
		}
		t.Logf("SQL: %s", sqlStr)

		rows, err := session.Run(context.Background(), "revenue_report")
		if err != nil {
			t.Fatalf("failed to run revenue_report: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var name string
			var totalRevenue, totalSurcharge, totalMiles, revPerMile float64
			if err := rows.Scan(&name, &totalRevenue, &totalSurcharge, &totalMiles, &revPerMile); err != nil {
				t.Fatalf("failed to scan row: %v", err)
			}
			count++
			if totalRevenue <= 0 {
				t.Errorf("expected positive total_revenue for %s, got %f", name, totalRevenue)
			}
			if totalSurcharge <= 0 {
				t.Errorf("expected positive total_surcharge for %s, got %f", name, totalSurcharge)
			}
			if totalMiles <= 0 {
				t.Errorf("expected positive total_miles for %s, got %f", name, totalMiles)
			}
			// Verify revPerMile is roughly totalRevenue / totalMiles
			expectedRevPerMile := totalRevenue / totalMiles
			if revPerMile < expectedRevPerMile-0.01 || revPerMile > expectedRevPerMile+0.01 {
				t.Errorf("mismatch in revenue_per_mile for %s: got %f, expected %f", name, revPerMile, expectedRevPerMile)
			}
		}
		if count == 0 {
			t.Errorf("expected rows in revenue_report, got 0")
		}
		t.Logf("Successfully processed %d customer revenue records", count)
	})

	t.Run("Compile and Run Revenue By Date", func(t *testing.T) {
		sqlStr, err := session.GetSQL("revenue_by_date")
		if err != nil {
			t.Fatalf("failed to compile revenue_by_date: %v", err)
		}
		t.Logf("SQL: %s", sqlStr)

		rows, err := session.Run(context.Background(), "revenue_by_date")
		if err != nil {
			t.Fatalf("failed to run revenue_by_date: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var tripDate time.Time
			var totalRevenue, revPerMile float64
			if err := rows.Scan(&tripDate, &totalRevenue, &revPerMile); err != nil {
				t.Fatalf("failed to scan row: %v", err)
			}
			count++
			if totalRevenue <= 0 {
				t.Errorf("expected positive total_revenue for %v, got %f", tripDate, totalRevenue)
			}
		}
		if count == 0 {
			t.Errorf("expected rows in revenue_by_date, got 0")
		}
		t.Logf("Successfully processed %d daily revenue records", count)
	})

	t.Run("Compile and Run Revenue By Date with Nested Routes", func(t *testing.T) {
		sqlStr, err := session.GetSQL("revenue_by_date_with_routes")
		if err != nil {
			t.Fatalf("failed to compile revenue_by_date_with_routes: %v", err)
		}
		t.Logf("SQL: %s", sqlStr)

		rows, err := session.Run(context.Background(), "revenue_by_date_with_routes")
		if err != nil {
			t.Fatalf("failed to run revenue_by_date_with_routes: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var tripDate time.Time
			var totalRevenue, revPerMile float64
			var routeDetailsRaw any
			if err := rows.Scan(&tripDate, &totalRevenue, &revPerMile, &routeDetailsRaw); err != nil {
				t.Fatalf("failed to scan row: %v", err)
			}
			count++
			if totalRevenue <= 0 {
				t.Errorf("expected positive total_revenue for %v, got %f", tripDate, totalRevenue)
			}
			if routeDetailsRaw == nil {
				t.Errorf("expected route_details to be non-nil for %v", tripDate)
			}
		}
		if count == 0 {
			t.Errorf("expected rows in revenue_by_date_with_routes, got 0")
		}
		t.Logf("Successfully processed %d daily revenue records with nested details", count)
	})
}
