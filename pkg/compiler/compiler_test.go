package compiler

import (
	"strings"
	"testing"

	"github.com/galihanggara68/mallow/pkg/ir"
)

func TestCompileBasicReduce(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"brand": {
				Kind: ir.KindDimension,
				Name: "brand",
				Type: ir.TypeString,
			},
			"sales": {
				Kind:       ir.KindMeasure,
				Name:       "sales",
				Type:       ir.TypeNumber,
				Expression: "SUM(sales)",
			},
		},
		PrimarySource: ir.PrimarySource{
			TablePath: "orders",
		},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"brand"},
				Measures:   []string{"sales"},
			},
		},
	}

	expected := "SELECT t0.\"brand\" AS \"brand\", SUM(sales) AS \"sales\" FROM \"orders\" AS t0 GROUP BY 1"
	got, err := comp.Compile(source, query)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if got != expected {
		t.Errorf("Compile() = %v, want %v", got, expected)
	}
}

func TestCompileMultipleFieldsReduce(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"brand": {
				Kind: ir.KindDimension,
				Name: "brand",
				Type: ir.TypeString,
			},
			"category": {
				Kind: ir.KindDimension,
				Name: "category",
				Type: ir.TypeString,
			},
			"sales": {
				Kind:       ir.KindMeasure,
				Name:       "sales",
				Type:       ir.TypeNumber,
				Expression: "SUM(sales)",
			},
			"count": {
				Kind:       ir.KindMeasure,
				Name:       "count",
				Type:       ir.TypeNumber,
				Expression: "COUNT(*)",
			},
		},
		PrimarySource: ir.PrimarySource{
			TablePath: "orders",
		},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"brand", "category"},
				Measures:   []string{"sales", "count"},
			},
		},
	}

	expected := "SELECT t0.\"brand\" AS \"brand\", t0.\"category\" AS \"category\", SUM(sales) AS \"sales\", COUNT(*) AS \"count\" FROM \"orders\" AS t0 GROUP BY 1,2"
	got, err := comp.Compile(source, query)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if got != expected {
		t.Errorf("Compile() = %v, want %v", got, expected)
	}
}

func TestCompileWithJoin(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	users := ir.SourceDef{
		Name: "users",
		Fields: map[string]ir.FieldDef{
			"id":   {Kind: ir.KindDimension, Name: "id"},
			"name": {Kind: ir.KindDimension, Name: "name"},
		},
		PrimarySource: ir.PrimarySource{TablePath: "users"},
	}

	source := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"id": {Kind: ir.KindDimension, Name: "id"},
			"user": {
				Kind:       ir.KindJoin,
				Name:       "user",
				JoinSource: &users,
				JoinOn: ir.BinaryExpr{
					Op:    ir.OpEq,
					Left:  ir.FieldReference{Path: []string{"user_id"}},
					Right: ir.FieldReference{Path: []string{"user", "id"}},
				},
			},
		},
		PrimarySource: ir.PrimarySource{TablePath: "orders"},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"id", "user_name"},
				InlineFields: map[string]ir.FieldDef{
					"user_name": {Kind: ir.KindDimension, Name: "user_name", Expr: ir.FieldReference{Path: []string{"user", "name"}}},
				},
			},
		},
	}

	expected := "SELECT t0.\"id\" AS \"id\", \"user\".\"name\" AS \"user_name\" FROM \"orders\" AS t0 LEFT JOIN \"users\" AS \"user\" ON (t0.\"user_id\" = \"user\".\"id\") GROUP BY 1,2"
	got, err := comp.Compile(source, query)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if got != expected {
		t.Errorf("Compile() = %v, want %v", got, expected)
	}
}

func TestCompilePipeline(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"brand": {Kind: ir.KindDimension, Name: "brand"},
			"sales": {Kind: ir.KindMeasure, Name: "sales", Expression: "SUM(sales)"},
		},
		PrimarySource: ir.PrimarySource{TablePath: "orders"},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"brand"},
				Measures:   []string{"sales"},
			},
			{
				Dimensions: []string{"brand"},
				Filters: []ir.Expr{ir.BinaryExpr{
					Op:    ir.OpGt,
					Left:  ir.FieldReference{Path: []string{"sales"}},
					Right: ir.Literal{Value: 100, Type: ir.TypeNumber},
				}},
			},
		},
	}

	// stage0: SELECT t0."brand" AS "brand", SUM(sales) AS "sales" FROM "orders" AS t0 GROUP BY 1
	// final: WITH stage0 AS (SELECT t0."brand" AS "brand", SUM(sales) AS "sales" FROM "orders" AS t0 GROUP BY 1) SELECT "brand" FROM "stage0" AS t1 WHERE ("sales" > 100) GROUP BY 1
	expected := "WITH stage0 AS (SELECT t0.\"brand\" AS \"brand\", SUM(sales) AS \"sales\" FROM \"orders\" AS t0 GROUP BY 1) SELECT \"brand\" FROM \"stage0\" AS t1 WHERE (t1.\"sales\" > 100) GROUP BY 1"
	got, err := comp.Compile(source, query)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if got != expected {
		t.Errorf("Compile() = %v, want %v", got, expected)
	}
}

func TestCompileWithAlias(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"brand": {
				Kind: ir.KindDimension,
				Name: "brand",
				As:   "b",
			},
			"sales": {
				Kind:       ir.KindMeasure,
				Name:       "sales",
				As:         "total_sales",
				Expression: "SUM(sales)",
			},
		},
		PrimarySource: ir.PrimarySource{
			TablePath: "orders",
		},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"brand"},
				Measures:   []string{"sales"},
			},
		},
	}

	expected := "SELECT t0.\"brand\" AS \"b\", SUM(sales) AS \"total_sales\" FROM \"orders\" AS t0 GROUP BY 1"
	got, err := comp.Compile(source, query)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if got != expected {
		t.Errorf("Compile() = %v, want %v", got, expected)
	}
}

func TestCompileWithFilters(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"brand": {
				Kind: ir.KindDimension,
				Name: "brand",
				Type: ir.TypeString,
			},
			"sales": {
				Kind:       ir.KindMeasure,
				Name:       "sales",
				Type:       ir.TypeNumber,
				Expression: "SUM(sales)",
			},
		},
		PrimarySource: ir.PrimarySource{
			TablePath: "orders",
		},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"brand"},
				Measures:   []string{"sales"},
				Filters: []ir.Expr{ir.BinaryExpr{
					Op:    ir.OpEq,
					Left:  ir.FieldReference{Path: []string{"brand"}},
					Right: ir.Literal{Value: "Apple", Type: ir.TypeString},
				}},
			},
		},
	}

	expected := "SELECT t0.\"brand\" AS \"brand\", SUM(sales) AS \"sales\" FROM \"orders\" AS t0 WHERE (t0.\"brand\" = 'Apple') GROUP BY 1"
	got, err := comp.Compile(source, query)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if got != expected {
		t.Errorf("Compile() = %v, want %v", got, expected)
	}
}

func TestCompileInlineFields(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"brand": {Kind: ir.KindDimension, Name: "brand"},
		},
		PrimarySource: ir.PrimarySource{TablePath: "orders"},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"brand", "branch"},
				Measures:   []string{"total_sales"},
				InlineFields: map[string]ir.FieldDef{
					"branch":      {Kind: ir.KindDimension, Name: "branch", Expr: ir.Literal{Value: "unknown", Type: ir.TypeString}},
					"total_sales": {Kind: ir.KindMeasure, Name: "total_sales", Expr: ir.CallExpr{Name: "sum", Args: []ir.Expr{ir.FieldReference{Path: []string{"sales"}}}}},
				},
			},
		},
	}

	expected := "SELECT t0.\"brand\" AS \"brand\", 'unknown' AS \"branch\", SUM(\"sales\") AS \"total_sales\" FROM \"orders\" AS t0 GROUP BY 1,2"
	got, err := comp.Compile(source, query)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if got != expected {
		t.Errorf("Compile() = %v, want %v", got, expected)
	}
}

func TestCompileTurducken(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"brand": {Kind: ir.KindDimension, Name: "brand"},
			"name":  {Kind: ir.KindDimension, Name: "name"},
			"sales": {Kind: ir.KindMeasure, Name: "sales", Expression: "SUM(sales)"},
		},
		PrimarySource: ir.PrimarySource{TablePath: "orders"},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"brand"},
				Nests:      []string{"top_items"},
				InlineFields: map[string]ir.FieldDef{
					"top_items": {
						Kind: ir.KindNest,
						Name: "top_items",
						NestedDef: &ir.Query{
							Stages: []ir.Stage{
								{
									Dimensions: []string{"name"},
									Measures:   []string{"sales"},
								},
							},
						},
					},
				},
			},
		},
	}

	expected := "SELECT t0.\"brand\" AS \"brand\", (SELECT JSON_GROUP_ARRAY(JSON_OBJECT('name', \"name\", 'sales', \"sales\")) FROM (SELECT t0n.\"name\" AS \"name\", SUM(sales) AS \"sales\" FROM \"orders\" AS t0n WHERE (t0n.\"brand\" = \"t0\".\"brand\") GROUP BY 1) AS sub) AS \"top_items\" FROM \"orders\" AS t0 GROUP BY 1"
	got, err := comp.Compile(source, query)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if got != expected {
		t.Errorf("Compile() = %v, want %v", got, expected)
	}
}

func TestCompileNestedTurducken(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name: "customers",
		Fields: map[string]ir.FieldDef{
			"id":   {Kind: ir.KindDimension, Name: "id"},
			"name": {Kind: ir.KindDimension, Name: "name"},
		},
		PrimarySource: ir.PrimarySource{TablePath: "customers"},
	}

	ordersSource := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"id":          {Kind: ir.KindDimension, Name: "id"},
			"customer_id": {Kind: ir.KindDimension, Name: "customer_id"},
			"amount":      {Kind: ir.KindMeasure, Name: "amount", Expression: "SUM(amount)"},
		},
		PrimarySource: ir.PrimarySource{TablePath: "orders"},
	}

	itemsSource := ir.SourceDef{
		Name: "items",
		Fields: map[string]ir.FieldDef{
			"id":       {Kind: ir.KindDimension, Name: "id"},
			"order_id": {Kind: ir.KindDimension, Name: "order_id"},
			"product":  {Kind: ir.KindDimension, Name: "product"},
			"qty":      {Kind: ir.KindMeasure, Name: "qty", Expression: "SUM(qty)"},
		},
		PrimarySource: ir.PrimarySource{TablePath: "items"},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"id", "name"},
				Nests:      []string{"orders_report"},
				InlineFields: map[string]ir.FieldDef{
					"orders_report": {
						Kind:       ir.KindNest,
						Name:       "orders_report",
						JoinSource: &ordersSource,
						NestedDef: &ir.Query{
							Stages: []ir.Stage{
								{
									Dimensions: []string{"id"},
									Measures:   []string{"amount"},
									Nests:      []string{"items_report"},
									InlineFields: map[string]ir.FieldDef{
										"items_report": {
											Kind:       ir.KindNest,
											Name:       "items_report",
											JoinSource: &itemsSource,
											NestedDef: &ir.Query{
												Stages: []ir.Stage{
													{
														Dimensions: []string{"product"},
														Measures:   []string{"qty"},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	got, err := comp.Compile(source, query)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	// Verify that 'items_report' is included in the JSON_OBJECT of the outer nest
	if !strings.Contains(got, "'items_report', \"items_report\"") {
		t.Errorf("Expected SQL to contain nested nest 'items_report', got: %s", got)
	}
}

func TestCompileErrorMissingField(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name:   "orders",
		Fields: map[string]ir.FieldDef{},
		PrimarySource: ir.PrimarySource{
			TablePath: "orders",
		},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"brand"},
			},
		},
	}

	_, err := comp.Compile(source, query)
	if err == nil {
		t.Error("expected error for missing field, got nil")
	}
}

func TestCompileErrorWrongKind(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"brand": {
				Kind: ir.KindMeasure, // Should be dimension
				Name: "brand",
			},
		},
		PrimarySource: ir.PrimarySource{
			TablePath: "orders",
		},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"brand"},
			},
		},
	}

	_, err := comp.Compile(source, query)
	if err == nil {
		t.Error("expected error for field kind mismatch, got nil")
	}
}

func TestCompileCastExpr(t *testing.T) {
	dialect := &DuckDBDialect{}
	comp := NewCompiler(dialect)

	source := ir.SourceDef{
		Name: "orders",
		Fields: map[string]ir.FieldDef{
			"price": {Kind: ir.KindDimension, Name: "price"},
		},
		PrimarySource: ir.PrimarySource{TablePath: "orders"},
	}

	query := ir.Query{
		Stages: []ir.Stage{
			{
				Dimensions: []string{"price_str"},
				InlineFields: map[string]ir.FieldDef{
					"price_str": {
						Kind: ir.KindDimension,
						Name: "price_str",
						Expr: ir.CastExpr{
							Expr: ir.FieldReference{Path: []string{"price"}},
							Type: ir.TypeString,
						},
					},
				},
			},
		},
	}

	expected := "SELECT CAST(\"price\" AS VARCHAR) AS \"price_str\" FROM \"orders\" AS t0 GROUP BY 1"
	got, err := comp.Compile(source, query)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if got != expected {
		t.Errorf("Compile() = %v, want %v", got, expected)
	}
}
