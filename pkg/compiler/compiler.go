package compiler

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/galihanggara68/mallow/pkg/ir"
)

// Dialect abstracts database-specific SQL generation.
type Dialect interface {
	QuoteIdentifier(id string) string
	DatePart(part string, expr string) string
	JSONArrayAgg(expr string) string
	JSONObject(pairs []string) string // pairs: "key", "value", "key", "value"...
	GetSchema(db *sql.DB, tableName string) (map[string]ir.DataType, error)
}

// Compiler translates IR to SQL.
type Compiler interface {
	Compile(source ir.SourceDef, query ir.Query) (string, error)
}

type sqlCompiler struct {
	dialect Dialect
}

// NewCompiler creates a new SQL compiler for the given dialect.
func NewCompiler(dialect Dialect) Compiler {
	return &sqlCompiler{dialect: dialect}
}

func (c *sqlCompiler) Compile(source ir.SourceDef, query ir.Query) (string, error) {
	if len(query.Stages) == 0 {
		return "", fmt.Errorf("query must have at least one stage")
	}

	var sql string
	var err error
	currentSource := source

	var ctes []string

	for i, stage := range query.Stages {
		if i == 0 {
			sql, err = c.compileStage(currentSource, stage, true, "t0", nil)
		} else {
			// For subsequent stages, we save the previous SQL as a CTE
			cteName := fmt.Sprintf("stage%d", i-1)
			ctes = append(ctes, fmt.Sprintf("%s AS (%s)", cteName, sql))

			// We need a dummy source for the next stage that points to the CTE
			currentSource = ir.SourceDef{
				Name: cteName,
				PrimarySource: ir.PrimarySource{
					TablePath: cteName,
				},
				Fields: make(map[string]ir.FieldDef),
			}
			sql, err = c.compileStage(currentSource, stage, false, fmt.Sprintf("t%d", i), nil)
			if err != nil {
				return "", err
			}
		}
		if err != nil {
			return "", err
		}
	}

	if len(ctes) > 0 {
		sql = fmt.Sprintf("WITH %s %s", strings.Join(ctes, ", "), sql)
	}

	return sql, nil
}

func (c *sqlCompiler) compileStage(source ir.SourceDef, stage ir.Stage, isFirstStage bool, sourceAlias string, correlation []ir.Expr) (string, error) {
	var selectCols []string
	var groupCols []string

	// Handle Dimensions
	for _, dimName := range stage.Dimensions {
		field, ok := stage.InlineFields[dimName]
		if !ok {
			field, ok = source.Fields[dimName]
		}
		if !ok {
			if isFirstStage {
				return "", fmt.Errorf("dimension not found: %s", dimName)
			}
			// For subsequent stages, we allow it
			selectCols = append(selectCols, c.dialect.QuoteIdentifier(dimName))
			groupCols = append(groupCols, fmt.Sprintf("%d", len(selectCols)))
			continue
		}
		if field.Kind != ir.KindDimension && field.Kind != ir.KindJoin && field.Kind != ir.KindJoinOne && field.Kind != ir.KindJoinMany { // Join fields can be dimensions too if they are just refs
			return "", fmt.Errorf("field is not a dimension: %s", dimName)
		}

		expr, err := c.compileField(field, sourceAlias)
		if err != nil {
			return "", err
		}

		// Prefix with source alias if it's a simple column ref
		if !strings.Contains(expr, "(") && !strings.Contains(expr, ".") && !strings.HasPrefix(expr, "'") && !strings.HasPrefix(expr, "-") && expr != "TRUE" && expr != "FALSE" {
			expr = fmt.Sprintf("%s.%s", sourceAlias, expr)
		}

		alias := ir.ActiveName(field)
		quotedName := c.dialect.QuoteIdentifier(field.Name)
		if alias != "" && (alias != field.Name || expr != quotedName) {
			selectCols = append(selectCols, fmt.Sprintf("%s AS %s", expr, c.dialect.QuoteIdentifier(alias)))
		} else {
			selectCols = append(selectCols, expr)
		}
		groupCols = append(groupCols, fmt.Sprintf("%d", len(selectCols)))
	}

	// Handle Measures
	for _, measureName := range stage.Measures {
		field, ok := stage.InlineFields[measureName]
		if !ok {
			field, ok = source.Fields[measureName]
		}
		if !ok {
			if isFirstStage {
				return "", fmt.Errorf("measure not found: %s", measureName)
			}
			selectCols = append(selectCols, c.dialect.QuoteIdentifier(measureName))
			continue
		}
		if field.Kind != ir.KindMeasure {
			return "", fmt.Errorf("field is not a measure: %s", measureName)
		}

		expr, err := c.compileField(field, sourceAlias)
		if err != nil {
			return "", err
		}

		// Note: we don't automatically prefix measures with sourceAlias because they usually contain aggregates
		// that refer to columns which should be prefixed. Our compileField/compileExpr handles that via FieldReference.

		alias := ir.ActiveName(field)
		if alias != "" {
			selectCols = append(selectCols, fmt.Sprintf("%s AS %s", expr, c.dialect.QuoteIdentifier(alias)))
		} else {
			selectCols = append(selectCols, expr)
		}
	}

	// Handle Nests (Turducken)
	for _, nestName := range stage.Nests {
		field, ok := stage.InlineFields[nestName]
		if !ok {
			field, ok = source.Fields[nestName]
		}
		if !ok {
			return "", fmt.Errorf("nest not found: %s", nestName)
		}
		if field.Kind != ir.KindNest {
			return "", fmt.Errorf("field is not a nest: %s", nestName)
		}

		// Determine correlation for the nest
		// The nest correlates on all current dimensions of the outer stage
		// that also exist in the nested source.
		var nestCorrelation []ir.Expr
		innerSource := source
		if field.JoinSource != nil {
			innerSource = *field.JoinSource
		}

		for _, dimName := range stage.Dimensions {
			// Only correlate if the dimension exists in the nested source
			if _, ok := innerSource.Fields[dimName]; ok {
				nestCorrelation = append(nestCorrelation, ir.BinaryExpr{
					Op:    ir.OpEq,
					Left:  ir.FieldReference{Path: []string{dimName}},
					Right: ir.FieldReference{Path: []string{sourceAlias, dimName}},
				})
			}
		}

		expr, err := c.compileNest(source, field, nestCorrelation, sourceAlias)
		if err != nil {
			return "", err
		}

		selectCols = append(selectCols, fmt.Sprintf("%s AS %s", expr, c.dialect.QuoteIdentifier(nestName)))
	}

	if len(selectCols) == 0 {
		return "", fmt.Errorf("stage must have at least one dimension, measure or nest")
	}

	fromTable := source.PrimarySource.TablePath
	if fromTable == "" {
		if source.PrimarySource.SQL != "" {
			fromTable = fmt.Sprintf("(%s)", source.PrimarySource.SQL)
		} else {
			return "", fmt.Errorf("source has no table or SQL")
		}
	} else {
		// If it looks like a function call (has parentheses) or a quoted string, or a file path for DuckDB
		isDuckDB := false
		if _, ok := c.dialect.(*DuckDBDialect); ok {
			isDuckDB = true
		}
		if _, ok := c.dialect.(*PostgresDialect); ok {
			// PostgresDialect embeds DuckDBDialect, but we might want to know it's NOT just DuckDB
			isDuckDB = false // Or handle specifically
		}

		if (strings.Contains(fromTable, "(") && strings.Contains(fromTable, ")")) ||
			(strings.HasPrefix(fromTable, "'") && strings.HasSuffix(fromTable, "'")) {
			// Leave as is
		} else if isDuckDB && (strings.Contains(fromTable, "/") || strings.HasSuffix(fromTable, ".csv") || strings.HasSuffix(fromTable, ".parquet")) {
			fromTable = fmt.Sprintf("'%s'", fromTable)
		} else {
			// Split table path by '.' and quote each part
			parts := strings.Split(fromTable, ".")
			var quotedParts []string
			for _, p := range parts {
				quotedParts = append(quotedParts, c.dialect.QuoteIdentifier(p))
			}
			fromTable = strings.Join(quotedParts, ".")
		}
	}

	sql := fmt.Sprintf("SELECT %s FROM %s AS %s", strings.Join(selectCols, ", "), fromTable, sourceAlias)

	// Handle Joins - only join what's used
	usedJoins := make(map[string]bool)
	// Check all dimensions and measures for field references
	for _, dimName := range stage.Dimensions {
		if f, ok := stage.InlineFields[dimName]; ok {
			c.collectUsedJoins(f.Expr, usedJoins)
		} else if f, ok := source.Fields[dimName]; ok {
			c.collectUsedJoins(f.Expr, usedJoins)
		}
	}
	for _, measureName := range stage.Measures {
		if f, ok := stage.InlineFields[measureName]; ok {
			c.collectUsedJoins(f.Expr, usedJoins)
		} else if f, ok := source.Fields[measureName]; ok {
			c.collectUsedJoins(f.Expr, usedJoins)
		}
	}
	for _, filter := range stage.Filters {
		c.collectUsedJoins(filter, usedJoins)
	}

	for _, field := range source.Fields {
		if field.Kind == ir.KindJoinOne || field.Kind == ir.KindJoinMany || field.Kind == ir.KindJoin {
			if usedJoins[field.Name] {
				if field.JoinSource != nil {
					joinTable := field.JoinSource.PrimarySource.TablePath
					parts := strings.Split(joinTable, ".")
					var quotedParts []string
					for _, p := range parts {
						quotedParts = append(quotedParts, c.dialect.QuoteIdentifier(p))
					}
					joinTable = strings.Join(quotedParts, ".")
					onExpr, err := c.compileExpr(field.JoinOn, sourceAlias, true)
					if err != nil {
						return "", err
					}
					sql += fmt.Sprintf(" LEFT JOIN %s AS %s ON %s", joinTable, c.dialect.QuoteIdentifier(field.Name), onExpr)
				}
			}
		}
	}

	var allFilters []string
	for _, filter := range correlation {
		f, err := c.compileExpr(filter, sourceAlias, true)
		if err != nil {
			return "", err
		}
		allFilters = append(allFilters, f)
	}
	for _, filter := range stage.Filters {
		f, err := c.compileExpr(filter, sourceAlias, true)
		if err != nil {
			return "", err
		}
		allFilters = append(allFilters, f)
	}

	if len(allFilters) > 0 {
		sql += fmt.Sprintf(" WHERE %s", strings.Join(allFilters, " AND "))
	}

	if len(groupCols) > 0 {
		sql += fmt.Sprintf(" GROUP BY %s", strings.Join(groupCols, ","))
	}

	if len(stage.OrderBy) > 0 {
		var orderCols []string
		for _, o := range stage.OrderBy {
			parts := strings.Fields(o)
			col := parts[0]
			dir := ""
			if len(parts) > 1 {
				dir = " " + parts[1]
			}
			orderCols = append(orderCols, fmt.Sprintf("%s%s", c.dialect.QuoteIdentifier(col), dir))
		}
		sql += fmt.Sprintf(" ORDER BY %s", strings.Join(orderCols, ", "))
	}

	if stage.Limit != nil {
		sql += fmt.Sprintf(" LIMIT %d", *stage.Limit)
	}

	return sql, nil
}

func (c *sqlCompiler) compileField(field ir.FieldDef, sourceAlias string) (string, error) {
	if field.Expr != nil {
		return c.compileExpr(field.Expr, sourceAlias, false)
	}
	if field.Expression != "" {
		return field.Expression, nil
	}
	return c.dialect.QuoteIdentifier(field.Name), nil
}

func (c *sqlCompiler) compileNest(source ir.SourceDef, field ir.FieldDef, correlation []ir.Expr, sourceAlias string) (string, error) {
	if field.NestedDef == nil {
		return "", fmt.Errorf("nest field %s has no nested definition", field.Name)
	}

	if len(field.NestedDef.Stages) == 0 {
		return "", fmt.Errorf("nested query %s has no stages", field.Name)
	}

	// Determine the source for the nested query
	innerSource := source
	if field.JoinSource != nil {
		innerSource = *field.JoinSource
	}

	// We compile the nested query as a correlated subquery.
	// For simplicity, we only support the first stage of the nested query for now.
	stage := field.NestedDef.Stages[0]

	// We need to build the inner SELECT and the aggregation
	// The aggregation should happen over the result of the nested query.

	// First, compile the inner query with correlation
	// Use a unique alias for the inner query to avoid collisions in nested nests
	innerAlias := sourceAlias + "n"
	innerSQL, err := c.compileStage(innerSource, stage, true, innerAlias, correlation)
	if err != nil {
		return "", err
	}

	var jsonPairs []string
	for _, dimName := range stage.Dimensions {
		jsonPairs = append(jsonPairs, fmt.Sprintf("'%s'", dimName))
		jsonPairs = append(jsonPairs, c.dialect.QuoteIdentifier(dimName))
	}
	for _, measureName := range stage.Measures {
		jsonPairs = append(jsonPairs, fmt.Sprintf("'%s'", measureName))
		jsonPairs = append(jsonPairs, c.dialect.QuoteIdentifier(measureName))
	}
	for _, nestName := range stage.Nests {
		jsonPairs = append(jsonPairs, fmt.Sprintf("'%s'", nestName))
		jsonPairs = append(jsonPairs, c.dialect.QuoteIdentifier(nestName))
	}

	objExpr := c.dialect.JSONObject(jsonPairs)
	aggExpr := c.dialect.JSONArrayAgg(objExpr)

	return fmt.Sprintf("(SELECT %s FROM (%s) AS sub)", aggExpr, innerSQL), nil
}

func (c *sqlCompiler) collectUsedJoins(expr ir.Expr, used map[string]bool) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case ir.FieldReference:
		if len(e.Path) > 1 {
			used[e.Path[0]] = true
		}
	case ir.BinaryExpr:
		c.collectUsedJoins(e.Left, used)
		c.collectUsedJoins(e.Right, used)
	case ir.UnaryExpr:
		c.collectUsedJoins(e.Child, used)
	case ir.CallExpr:
		for _, arg := range e.Args {
			c.collectUsedJoins(arg, used)
		}
	case *ir.BinaryExpr:
		c.collectUsedJoins(*e, used)
	case *ir.UnaryExpr:
		c.collectUsedJoins(*e, used)
	case *ir.Literal:
		// literals don't use joins
	case *ir.FieldReference:
		c.collectUsedJoins(*e, used)
	case *ir.CallExpr:
		c.collectUsedJoins(*e, used)
	}
}

func (c *sqlCompiler) compileExpr(expr ir.Expr, sourceAlias string, useBaseAlias bool) (string, error) {
	switch e := expr.(type) {
	case ir.Literal:
		switch e.Type {
		case ir.TypeString:
			return fmt.Sprintf("'%s'", strings.ReplaceAll(fmt.Sprintf("%v", e.Value), "'", "''")), nil
		case ir.TypeNumber:
			return fmt.Sprintf("%v", e.Value), nil
		case ir.TypeBoolean:
			if val, ok := e.Value.(bool); ok && val {
				return "TRUE", nil
			}
			return "FALSE", nil
		default:
			return fmt.Sprintf("%v", e.Value), nil
		}
	case ir.FieldReference:
		if len(e.Path) == 1 && sourceAlias != "" && useBaseAlias {
			return fmt.Sprintf("%s.%s", sourceAlias, c.dialect.QuoteIdentifier(e.Path[0])), nil
		}
		var quotedPath []string
		for _, p := range e.Path {
			quotedPath = append(quotedPath, c.dialect.QuoteIdentifier(p))
		}
		return strings.Join(quotedPath, "."), nil
	case ir.BinaryExpr:
		left, err := c.compileExpr(e.Left, sourceAlias, useBaseAlias)
		if err != nil {
			return "", err
		}
		right, err := c.compileExpr(e.Right, sourceAlias, useBaseAlias)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(%s %s %s)", left, e.Op, right), nil
	case ir.UnaryExpr:
		child, err := c.compileExpr(e.Child, sourceAlias, useBaseAlias)
		if err != nil {
			return "", err
		}
		if e.Op == ir.OpNot {
			return fmt.Sprintf("NOT (%s)", child), nil
		}
		return fmt.Sprintf("%s(%s)", e.Op, child), nil
	case ir.CallExpr:
		blueprint, ok := functionRegistry[e.Name]
		if !ok {
			return "", fmt.Errorf("unknown function: %s", e.Name)
		}
		var args []string
		for _, arg := range e.Args {
			compiledArg, err := c.compileExpr(arg, sourceAlias, useBaseAlias)
			if err != nil {
				return "", err
			}
			args = append(args, compiledArg)
		}
		return blueprint(c.dialect, args)
	case *ir.BinaryExpr:
		return c.compileExpr(*e, sourceAlias, useBaseAlias)
	case *ir.UnaryExpr:
		return c.compileExpr(*e, sourceAlias, useBaseAlias)
	case *ir.Literal:
		return c.compileExpr(*e, sourceAlias, useBaseAlias)
	case *ir.FieldReference:
		return c.compileExpr(*e, sourceAlias, useBaseAlias)
	case *ir.CallExpr:
		return c.compileExpr(*e, sourceAlias, useBaseAlias)
	default:
		return "", fmt.Errorf("unsupported expression type: %T", expr)
	}
}

// DuckDBDialect implements the Dialect interface for DuckDB.
type DuckDBDialect struct{}

func (d *DuckDBDialect) QuoteIdentifier(id string) string {
	// Simple double-quoting for DuckDB.
	// In a real implementation, this would handle escaping internal double quotes.
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(id, "\"", "\"\""))
}

func (d *DuckDBDialect) DatePart(part string, expr string) string {
	return fmt.Sprintf("EXTRACT(%s FROM %s)", part, expr)
}

func (d *DuckDBDialect) JSONArrayAgg(expr string) string {
	return fmt.Sprintf("JSON_GROUP_ARRAY(%s)", expr)
}

func (d *DuckDBDialect) JSONObject(pairs []string) string {
	return fmt.Sprintf("JSON_OBJECT(%s)", strings.Join(pairs, ", "))
}

func (d *DuckDBDialect) GetSchema(db *sql.DB, tableName string) (map[string]ir.DataType, error) {
	actualTable := tableName
	if !strings.Contains(actualTable, "(") && !strings.Contains(actualTable, ")") && !strings.HasPrefix(actualTable, "'") {
		if strings.Contains(actualTable, "/") || strings.HasSuffix(actualTable, ".csv") || strings.HasSuffix(actualTable, ".parquet") {
			actualTable = fmt.Sprintf("'%s'", actualTable)
		}
	}

	rows, err := db.Query(fmt.Sprintf("DESCRIBE %s", actualTable))
	if err != nil {
		rows, err = db.Query(fmt.Sprintf("SELECT * FROM %s LIMIT 0", actualTable))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		colTypes, err := rows.ColumnTypes()
		if err != nil {
			return nil, err
		}
		schemaMap := make(map[string]ir.DataType)
		for _, col := range colTypes {
			schemaMap[col.Name()] = duckdbTypeToMallowType(col.DatabaseTypeName())
		}
		return schemaMap, nil
	}
	defer rows.Close()

	schemaMap := make(map[string]ir.DataType)
	for rows.Next() {
		var colName, colType, null, key, defVal, extra sql.NullString
		if err := rows.Scan(&colName, &colType, &null, &key, &defVal, &extra); err != nil {
			return nil, err
		}
		if colName.Valid && colType.Valid {
			schemaMap[colName.String] = duckdbTypeToMallowType(colType.String)
		}
	}
	return schemaMap, nil
}

func duckdbTypeToMallowType(dbType string) ir.DataType {
	dbType = strings.ToLower(dbType)
	switch {
	case strings.Contains(dbType, "varchar") || strings.Contains(dbType, "text") || strings.Contains(dbType, "char") || strings.Contains(dbType, "blob"):
		return ir.TypeString
	case strings.Contains(dbType, "int") || strings.Contains(dbType, "hugeint") || strings.Contains(dbType, "numeric") || strings.Contains(dbType, "decimal") || strings.Contains(dbType, "double") || strings.Contains(dbType, "float") || strings.Contains(dbType, "real"):
		return ir.TypeNumber
	case strings.Contains(dbType, "bool"):
		return ir.TypeBoolean
	case dbType == "date":
		return ir.TypeDate
	case strings.Contains(dbType, "timestamp") || strings.Contains(dbType, "time"):
		return ir.TypeTimestamp
	default:
		return ir.TypeUnknown
	}
}

// PostgresDialect implements the Dialect interface for Postgres.
type PostgresDialect struct {
	DuckDBDialect
}

func (d *PostgresDialect) DatePart(part string, expr string) string {
	return fmt.Sprintf("DATE_PART('%s', %s)", strings.ToLower(part), expr)
}

func (d *PostgresDialect) JSONArrayAgg(expr string) string {
	return fmt.Sprintf("JSON_AGG(%s)", expr)
}

func (d *PostgresDialect) JSONObject(pairs []string) string {
	return fmt.Sprintf("JSON_BUILD_OBJECT(%s)", strings.Join(pairs, ", "))
}

func (d *PostgresDialect) GetSchema(db *sql.DB, tableName string) (map[string]ir.DataType, error) {
	parts := strings.Split(tableName, ".")
	var schema, table string
	if len(parts) == 2 {
		schema = parts[0]
		table = parts[1]
	} else {
		schema = "public"
		table = tableName
	}

	query := `
		SELECT column_name, data_type 
		FROM information_schema.columns 
		WHERE table_name = $1 AND table_schema = $2
	`
	rows, err := db.Query(query, table, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	schemaMap := make(map[string]ir.DataType)
	for rows.Next() {
		var colName, dataType string
		if err := rows.Scan(&colName, &dataType); err != nil {
			return nil, err
		}
		schemaMap[colName] = postgresTypeToMallowType(dataType)
	}
	return schemaMap, nil
}

func postgresTypeToMallowType(pgType string) ir.DataType {
	pgType = strings.ToLower(pgType)
	switch {
	case strings.Contains(pgType, "char") || strings.Contains(pgType, "text") || strings.Contains(pgType, "uuid"):
		return ir.TypeString
	case strings.Contains(pgType, "int") || strings.Contains(pgType, "numeric") || strings.Contains(pgType, "decimal") || strings.Contains(pgType, "double") || strings.Contains(pgType, "real") || strings.Contains(pgType, "float"):
		return ir.TypeNumber
	case strings.Contains(pgType, "bool"):
		return ir.TypeBoolean
	case pgType == "date":
		return ir.TypeDate
	case strings.Contains(pgType, "timestamp") || strings.Contains(pgType, "time"):
		return ir.TypeTimestamp
	default:
		return ir.TypeUnknown
	}
}
