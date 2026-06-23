package translator

import (
	"testing"

	"github.com/galihanggara68/mallow/pkg/ir"
)

func TestParserBasicSource(t *testing.T) {
	input := `source: orders is table('db.orders') {
		dimension: brand
		measure: sales
	}`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(mallow.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(mallow.Statements))
	}

	stmt := mallow.Statements[0]
	if stmt.Source == nil {
		t.Fatal("Expected source statement")
	}

	if stmt.Source.Name != "orders" {
		t.Errorf("Expected source name 'orders', got '%s'", stmt.Source.Name)
	}

	if *stmt.Source.Def.Table != "db.orders" {
		t.Errorf("Expected table 'db.orders', got '%s'", *stmt.Source.Def.Table)
	}

	if len(stmt.Source.Body.Items) != 2 {
		t.Errorf("Expected 2 items in body, got %d", len(stmt.Source.Body.Items))
	}
}

func TestTranslateBasicSource(t *testing.T) {
	input := `source: orders is table('db.orders') {
		dimension: brand
		measure: sales
	}`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	sources, _, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	if len(sources) != 1 {
		t.Fatalf("Expected 1 source, got %d", len(sources))
	}

	src := sources[0]
	if src.Name != "orders" {
		t.Errorf("Expected name 'orders', got '%s'", src.Name)
	}

	if src.PrimarySource.TablePath != "db.orders" {
		t.Errorf("Expected table 'db.orders', got '%s'", src.PrimarySource.TablePath)
	}

	if f, ok := src.Fields["brand"]; !ok || f.Kind != ir.KindDimension {
		t.Error("Missing or incorrect dimension 'brand'")
	}

	if f, ok := src.Fields["sales"]; !ok || f.Kind != ir.KindMeasure {
		t.Error("Missing or incorrect measure 'sales'")
	}
}

func TestTranslateSourceRef(t *testing.T) {
	input := `
		source: base is table('db.orders')
		source: orders is base {
			dimension: brand
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	sources, _, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	if len(sources) != 2 {
		t.Fatalf("Expected 2 sources, got %d", len(sources))
	}

	orders := sources[1]
	if orders.PrimarySource.TablePath != "db.orders" {
		t.Errorf("Expected table 'db.orders' (inherited), got '%s'", orders.PrimarySource.TablePath)
	}

	if _, ok := orders.Fields["brand"]; !ok {
		t.Error("Missing dimension 'brand'")
	}
}

func TestTranslateSourceWithExpressions(t *testing.T) {
	input := `source: orders is table('db.orders') {
		dimension: revenue is price * quantity
		measure: total_revenue is sum(revenue)
	}`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	sources, _, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	orders := sources[0]
	revenue := orders.Fields["revenue"]
	if revenue.Expr == nil {
		t.Fatal("Expected expression for revenue")
	}

	be, ok := revenue.Expr.(ir.BinaryExpr)
	if !ok {
		t.Fatalf("Expected binary expression, got %T", revenue.Expr)
	}
	if be.Op != ir.OpMul {
		t.Errorf("Expected OpMul, got %v", be.Op)
	}

	totalRevenue := orders.Fields["total_revenue"]
	if totalRevenue.Expr == nil {
		t.Fatal("Expected expression for total_revenue")
	}
	ce, ok := totalRevenue.Expr.(ir.CallExpr)
	if !ok {
		t.Fatalf("Expected call expression, got %T", totalRevenue.Expr)
	}
	if ce.Name != "sum" {
		t.Errorf("Expected function 'sum', got '%s'", ce.Name)
	}
}

func TestTranslateQuery(t *testing.T) {
	input := `
		source: orders is table('db.orders') {
			dimension: brand
			measure: sales is sum(amount)
		}
		query: brand_sales is orders -> {
			group_by: brand
			aggregate: sales
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	_, queries, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	if len(queries) != 1 {
		t.Fatalf("Expected 1 query, got %d", len(queries))
	}

	q := queries[0]
	if len(q.Stages) != 1 {
		t.Fatalf("Expected 1 stage, got %d", len(q.Stages))
	}
	stage := q.Stages[0]
	if len(stage.Dimensions) != 1 || stage.Dimensions[0] != "brand" {
		t.Errorf("Expected dimension 'brand', got %v", stage.Dimensions)
	}
	if len(stage.Measures) != 1 || stage.Measures[0] != "sales" {
		t.Errorf("Expected measure 'sales', got %v", stage.Measures)
	}
}

func TestTranslateJoin(t *testing.T) {
	input := `
		source: users is table('db.users')
		source: orders is table('db.orders') {
			join_one: user is users on user_id = user.id
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	sources, _, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	orders := sources[1]
	userJoin, ok := orders.Fields["user"]
	if !ok || userJoin.Kind != ir.KindJoinOne {
		t.Fatal("Missing or incorrect join 'user'")
	}
	if userJoin.JoinSource == nil || userJoin.JoinSource.Name != "users" {
		t.Errorf("Expected join source 'users', got %v", userJoin.JoinSource)
	}
	if userJoin.JoinOn == nil {
		t.Fatal("Expected join_on expression")
	}
}

func TestTranslatePipeline(t *testing.T) {
	input := `
		source: orders is table('db.orders') {
			dimension: brand
			measure: sales is sum(amount)
		}
		query: top_brands is orders -> {
			group_by: brand
			aggregate: sales
		} -> {
			project: brand
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	_, queries, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	q := queries[0]
	if len(q.Stages) != 2 {
		t.Fatalf("Expected 2 stages, got %d", len(q.Stages))
	}
}

func TestTranslateNest(t *testing.T) {
	input := `
		source: orders is table('db.orders') {
			dimension: brand
			measure: sales is sum(amount)
		}
		query: brand_report is orders -> {
			group_by: brand
			nest: top_items is {
				group_by: brand
			}
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	_, queries, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	q := queries[0]
	if len(q.Stages[0].Nests) != 1 || q.Stages[0].Nests[0] != "top_items" {
		t.Errorf("Expected nest 'top_items', got %v", q.Stages[0].Nests)
	}
}

func TestTranslateNestWithRef(t *testing.T) {
	input := `
		source: items is table('db.items') {
			dimension: name
		}
		source: orders is table('db.orders') {
			dimension: id
			join_one: item is items on item_id = item.id
		}
		query: order_report is orders -> {
			group_by: id
			nest: item_detail is item -> {
				group_by: name
			}
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	_, queries, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	q := queries[0]
	nestField := q.Stages[0].InlineFields["item_detail"]
	if nestField.Kind != ir.KindNest {
		t.Fatalf("Expected nest kind, got %v", nestField.Kind)
	}
	if len(nestField.NestedDef.Stages[0].Dimensions) != 1 || nestField.NestedDef.Stages[0].Dimensions[0] != "name" {
		t.Errorf("Expected nested dimension 'name', got %v", nestField.NestedDef.Stages[0].Dimensions)
	}
}

func TestTranslateQueryInvalidField(t *testing.T) {
	input := `
		source: orders is table('db.orders') {
			dimension: brand
		}
		query: brand_sales is orders -> {
			group_by: unknown_field
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	_, _, err = tr.Translate(mallow)
	if err == nil {
		t.Error("Expected error for unknown field, got nil")
	}
}

func TestTranslateComplexPipeline(t *testing.T) {
	input := `
		source: cc is table('datamart.cc_records') {
			dimension: issuing_bank, branch
			measure: card_count is count()
		}
		query: complex_q is cc -> {
			group_by: issuing_bank, branch, year is 2024
			aggregate: card_count, total_limit is sum(credit_limit)
		} -> {
			group_by: issuing_bank
			aggregate: avg_limit is avg(total_limit)
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	_, queries, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	q := queries[0]
	if len(q.Stages) != 2 {
		t.Fatalf("Expected 2 stages, got %d", len(q.Stages))
	}

	s1 := q.Stages[0]
	if len(s1.Dimensions) != 3 || len(s1.Measures) != 2 {
		t.Errorf("Stage 1: expected 3 dims and 2 measures, got %d and %d", len(s1.Dimensions), len(s1.Measures))
	}

	s2 := q.Stages[1]
	if len(s2.Dimensions) != 1 || len(s2.Measures) != 1 {
		t.Errorf("Stage 2: expected 1 dim and 1 measure, got %d and %d", len(s2.Dimensions), len(s2.Measures))
	}
}

func TestTranslateMultipleDimensionsAndMeasures(t *testing.T) {
	input := `
		source: cc is table('datamart.cc_records') {
			dimension:
				issuing_bank
				other_dim is 1
			measure:
				card_count is count()
				limit_sum is sum(credit_limit)
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	sources, _, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	if len(sources) != 1 {
		t.Fatalf("Expected 1 source, got %d", len(sources))
	}

	src := sources[0]
	if _, ok := src.Fields["issuing_bank"]; !ok {
		t.Error("Missing dimension 'issuing_bank'")
	}
	if f, ok := src.Fields["other_dim"]; !ok || f.Kind != ir.KindDimension {
		t.Error("Missing or incorrect dimension 'other_dim'")
	}
	if f, ok := src.Fields["card_count"]; !ok || f.Kind != ir.KindMeasure {
		t.Error("Missing or incorrect measure 'card_count'")
	}
	if f, ok := src.Fields["limit_sum"]; !ok || f.Kind != ir.KindMeasure {
		t.Error("Missing or incorrect measure 'limit_sum'")
	}
}

func TestTranslateQueryMultipleFieldsWithoutCommas(t *testing.T) {
	input := `
		source: cc is table('datamart.cc_records') {
			dimension:
				issuing_bank
				branch
			measure:
				card_count is count()
				limit_sum is sum(credit_limit)
		}

		query: top_banks is cc -> {
			group_by:
				issuing_bank
				branch
			aggregate:
				card_count
				limit_sum
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	_, queries, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	if len(queries) != 1 {
		t.Fatalf("Expected 1 query, got %d", len(queries))
	}

	q := queries[0]
	if len(q.Stages) != 1 {
		t.Fatalf("Expected 1 stage, got %d", len(q.Stages))
	}

	stage := q.Stages[0]
	if len(stage.Dimensions) != 2 || stage.Dimensions[0] != "issuing_bank" || stage.Dimensions[1] != "branch" {
		t.Errorf("Expected dimensions ['issuing_bank', 'branch'], got %v", stage.Dimensions)
	}
	if len(stage.Measures) != 2 || stage.Measures[0] != "card_count" || stage.Measures[1] != "limit_sum" {
		t.Errorf("Expected measures ['card_count', 'limit_sum'], got %v", stage.Measures)
	}
}

func TestTranslateCommaSeparatedFields(t *testing.T) {
	input := `
		source: cc is table('datamart.cc_records') {
			dimension: issuing_bank, branch
			measure: card_count is count(), limit_sum is sum(credit_limit)
		}
		query: q is cc -> {
			group_by: issuing_bank, branch
			aggregate: card_count, limit_sum
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	sources, queries, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	src := sources[0]
	if len(src.Fields) != 4 {
		t.Errorf("Expected 4 fields in source, got %d", len(src.Fields))
	}

	q := queries[0]
	stage := q.Stages[0]
	if len(stage.Dimensions) != 2 || len(stage.Measures) != 2 {
		t.Errorf("Expected 2 dims and 2 measures in query, got %d and %d", len(stage.Dimensions), len(stage.Measures))
	}
}

func TestTranslateQueryInlineExpressions(t *testing.T) {
	input := `
		source: cc is table('datamart.cc_records') {
			dimension: issuing_bank
		}
		query: q is cc -> {
			group_by: issuing_bank, branch is 'unknown'
			aggregate: card_count is count(), total_limit is sum(credit_limit)
		}
	`
	mallow, err := Parser.ParseString("", input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tr := NewTranslator()
	_, queries, err := tr.Translate(mallow)
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}

	q := queries[0]
	stage := q.Stages[0]

	if len(stage.InlineFields) != 3 {
		t.Errorf("Expected 3 inline fields, got %d", len(stage.InlineFields))
	}

	if f, ok := stage.InlineFields["branch"]; !ok || f.Kind != ir.KindDimension {
		t.Error("Missing inline dimension 'branch'")
	}
	if f, ok := stage.InlineFields["card_count"]; !ok || f.Kind != ir.KindMeasure {
		t.Error("Missing inline measure 'card_count'")
	}
	if f, ok := stage.InlineFields["total_limit"]; !ok || f.Kind != ir.KindMeasure {
		t.Error("Missing inline measure 'total_limit'")
	}
}
