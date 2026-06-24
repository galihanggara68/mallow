package translator

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/galihanggara68/mallow/pkg/ir"
)

// FieldSpace is a symbol table for Mallow scopes.
type FieldSpace interface {
	Lookup(name string) (ir.FieldDef, bool)
	Parent() FieldSpace
}

type baseFieldSpace struct {
	parent FieldSpace
	fields map[string]ir.FieldDef
}

func (fs *baseFieldSpace) Lookup(name string) (ir.FieldDef, bool) {
	if f, ok := fs.fields[name]; ok {
		return f, true
	}
	if fs.parent != nil {
		return fs.parent.Lookup(name)
	}
	return ir.FieldDef{}, false
}

func (fs *baseFieldSpace) Parent() FieldSpace {
	return fs.parent
}

type SourceFieldSpace struct {
	baseFieldSpace
	source ir.SourceDef
}

func NewSourceFieldSpace(source ir.SourceDef, parent FieldSpace) *SourceFieldSpace {
	fs := &SourceFieldSpace{
		baseFieldSpace: baseFieldSpace{
			parent: parent,
			fields: source.Fields,
		},
		source: source,
	}
	return fs
}

// GlobalFieldSpace stores top-level sources.
type GlobalFieldSpace struct {
	baseFieldSpace
	sources map[string]ir.SourceDef
}

func NewGlobalFieldSpace() *GlobalFieldSpace {
	return &GlobalFieldSpace{
		baseFieldSpace: baseFieldSpace{
			fields: make(map[string]ir.FieldDef),
		},
		sources: make(map[string]ir.SourceDef),
	}
}

func (g *GlobalFieldSpace) RegisterSource(name string, source ir.SourceDef) {
	g.sources[name] = source
}

func (g *GlobalFieldSpace) GetSource(name string) (ir.SourceDef, bool) {
	s, ok := g.sources[name]
	return s, ok
}

type SchemaIntrospector interface {
	GetSchema(db *sql.DB, tableName string) (map[string]ir.DataType, error)
}

// Translator converts AST to IR.
type Translator struct {
	global  *GlobalFieldSpace
	db      *sql.DB
	dialect SchemaIntrospector
}

func NewTranslator() *Translator {
	return &Translator{
		global: NewGlobalFieldSpace(),
	}
}

func (t *Translator) SetDB(db *sql.DB, dialect SchemaIntrospector) {
	t.db = db
	t.dialect = dialect
}

func (t *Translator) Translate(m *Mallow) ([]ir.SourceDef, []ir.Query, error) {
	var sources []ir.SourceDef
	var queries []ir.Query
	for _, stmt := range m.Statements {
		if stmt.Source != nil {
			src, err := t.translateSource(stmt.Source)
			if err != nil {
				return nil, nil, err
			}
			t.global.RegisterSource(src.Name, src)
			sources = append(sources, src)
		} else if stmt.Query != nil {
			q, err := t.translateQuery(stmt.Query)
			if err != nil {
				return nil, nil, err
			}
			queries = append(queries, q)
		} else if stmt.Run != nil {
			q, err := t.translateRun(stmt.Run)
			if err != nil {
				return nil, nil, err
			}
			queries = append(queries, q)
		}
	}
	return sources, queries, nil
}

func (t *Translator) translateRun(decl *RunDeclaration) (ir.Query, error) {
	return t.translateQueryDef(decl.Def)
}

func (t *Translator) translateQuery(decl *QueryDeclaration) (ir.Query, error) {
	q, err := t.translateQueryDef(decl.Def)
	if err == nil {
		q.Name = decl.Name
	}
	return q, err
}

func (t *Translator) translateQueryDef(def *QueryDef) (ir.Query, error) {
	q := ir.Query{SourceName: def.Source}
	// We need the source to validate fields
	src, ok := t.global.GetSource(def.Source)
	if !ok {
		return ir.Query{}, fmt.Errorf("source not found for query: %s", def.Source)
	}

	currentFS := NewSourceFieldSpace(src, t.global)

	for _, astStage := range def.Stages {
		stage, err := t.translateStageItems(astStage.Items, currentFS)
		if err != nil {
			return ir.Query{}, err
		}
		q.Stages = append(q.Stages, stage)

		// Update FS for next stage (output of this stage becomes input of next)
		// For MVP, we'll just use a simplified FS that allows any field named in previous stage.
		// ...
	}

	return q, nil
}

func (t *Translator) translateStageItems(items []*QueryItem, fs FieldSpace) (ir.Stage, error) {
	stage := ir.Stage{
		InlineFields: make(map[string]ir.FieldDef),
	}
	var hasGroupByOrAggregate bool
	var hasProject bool

	for _, item := range items {
		if len(item.GroupBy) > 0 {
			hasGroupByOrAggregate = true
			for _, dim := range item.GroupBy {
				cleanName := stripBackticks(dim.Name)
				if dim.Expr != nil {
					expr, err := t.translateExpr(dim.Expr)
					if err != nil {
						return ir.Stage{}, err
					}
					stage.InlineFields[cleanName] = ir.FieldDef{
						Kind: ir.KindDimension,
						Name: cleanName,
						Expr: expr,
					}
				} else {
					f, ok := fs.Lookup(cleanName)
					if !ok {
						return ir.Stage{}, fmt.Errorf("dimension not found: %s", cleanName)
					}

					if f.Kind != ir.KindDimension && f.Kind != ir.KindJoin && f.Kind != ir.KindJoinOne && f.Kind != ir.KindJoinMany {
						return ir.Stage{}, fmt.Errorf("field is not a dimension: %s", cleanName)
					}
				}
				stage.Dimensions = append(stage.Dimensions, cleanName)
			}
		}
		if len(item.Aggregate) > 0 {
			hasGroupByOrAggregate = true
			for _, meas := range item.Aggregate {
				cleanName := stripBackticks(meas.Name)
				if meas.Expr != nil {
					expr, err := t.translateExpr(meas.Expr)
					if err != nil {
						return ir.Stage{}, err
					}
					stage.InlineFields[cleanName] = ir.FieldDef{
						Kind: ir.KindMeasure,
						Name: cleanName,
						Expr: expr,
					}
				} else {
					f, ok := fs.Lookup(cleanName)
					if !ok {
						return ir.Stage{}, fmt.Errorf("measure not found: %s", cleanName)
					}
					if f.Kind != ir.KindMeasure {
						return ir.Stage{}, fmt.Errorf("field is not a measure: %s", cleanName)
					}
				}
				stage.Measures = append(stage.Measures, cleanName)
			}
		}
		if len(item.Project) > 0 {
			hasProject = true
			for _, dim := range item.Project {
				cleanName := stripBackticks(dim.Name)
				if dim.Expr != nil {
					expr, err := t.translateExpr(dim.Expr)
					if err != nil {
						return ir.Stage{}, err
					}
					stage.InlineFields[cleanName] = ir.FieldDef{
						Kind: ir.KindDimension,
						Name: cleanName,
						Expr: expr,
					}
				} else {
					if _, ok := fs.Lookup(cleanName); !ok {
						return ir.Stage{}, fmt.Errorf("field not found: %s", cleanName)
					}
				}
				stage.Dimensions = append(stage.Dimensions, cleanName)
			}
		}
		if len(item.Nest) > 0 {
			hasGroupByOrAggregate = true
			for _, nest := range item.Nest {
				// Translate nested query
				cleanNestName := stripBackticks(nest.Name)
				sourceRef := ""
				if nest.Ref != nil {
					sourceRef = stripBackticks(*nest.Ref)
				}

				nestDef := &QueryDef{
					Source: sourceRef,
					Stages: []*QueryBody{nest.Body},
				}

				var nestedFS FieldSpace = fs
				var joinSource *ir.SourceDef
				if sourceRef != "" {
					// If there's a source ref, we need to look up that source
					f, ok := fs.Lookup(sourceRef)
					if ok && f.JoinSource != nil {
						nestedFS = NewSourceFieldSpace(*f.JoinSource, t.global)
						joinSource = f.JoinSource
					} else {
						// Fallback to global source
						if src, ok := t.global.GetSource(sourceRef); ok {
							nestedFS = NewSourceFieldSpace(src, t.global)
							joinSource = &src
						}
					}
				}

				nestedQuery, err := t.translateNestedQuery(nestDef, nestedFS)
				if err != nil {
					return ir.Stage{}, err
				}

				stage.Nests = append(stage.Nests, cleanNestName)
				stage.InlineFields[cleanNestName] = ir.FieldDef{
					Kind:       ir.KindNest,
					Name:       cleanNestName,
					NestedDef:  &nestedQuery,
					JoinSource: joinSource,
				}
			}
		}
		if len(item.Where) > 0 {
			for _, expr := range item.Where {
				translatedExpr, err := t.translateExpr(expr)
				if err != nil {
					return ir.Stage{}, err
				}
				stage.Filters = append(stage.Filters, translatedExpr)
			}
		}
	}

	stage.IsProject = hasProject && !hasGroupByOrAggregate

	for _, item := range items {
		if len(item.OrderBy) > 0 {
			for _, o := range item.OrderBy {
				cleanName := stripBackticks(o.Name)
				_, inFS := fs.Lookup(cleanName)
				_, inInline := stage.InlineFields[cleanName]
				inDims := false
				for _, d := range stage.Dimensions {
					if d == cleanName {
						inDims = true
						break
					}
				}
				inMeas := false
				for _, m := range stage.Measures {
					if m == cleanName {
						inMeas = true
						break
					}
				}
				if !inFS && !inInline && !inDims && !inMeas {
					return ir.Stage{}, fmt.Errorf("order_by field not found: %s", cleanName)
				}
				orderStr := cleanName
				if o.Direction != "" {
					orderStr += " " + strings.ToUpper(o.Direction)
				}
				stage.OrderBy = append(stage.OrderBy, orderStr)
			}
		}
		if item.Limit != nil {
			stage.Limit = item.Limit
		}
	}

	return stage, nil
}

func (t *Translator) translateNestedQuery(def *QueryDef, fs FieldSpace) (ir.Query, error) {
	q := ir.Query{}
	for _, astStage := range def.Stages {
		stage, err := t.translateStageItems(astStage.Items, fs)
		if err != nil {
			return ir.Query{}, err
		}
		q.Stages = append(q.Stages, stage)
	}
	return q, nil
}

func (t *Translator) translateSource(decl *SourceDeclaration) (ir.SourceDef, error) {
	cleanSourceName := stripBackticks(decl.Name)
	src := ir.SourceDef{
		Name:   cleanSourceName,
		Fields: make(map[string]ir.FieldDef),
	}

	if decl.Def.Table != nil {
		src.PrimarySource.TablePath = *decl.Def.Table
	} else if decl.Def.Ref != nil {
		cleanRef := stripBackticks(*decl.Def.Ref)
		parentSrc, ok := t.global.GetSource(cleanRef)
		if !ok {
			return ir.SourceDef{}, fmt.Errorf("source not found: %s", cleanRef)
		}
		// Clone parent fields
		for k, v := range parentSrc.Fields {
			src.Fields[k] = v
		}
		src.PrimarySource = parentSrc.PrimarySource
	}

	if decl.Body != nil {
		for _, item := range decl.Body.Items {
			if len(item.Dimensions) > 0 {
				for _, dim := range item.Dimensions {
					cleanName := stripBackticks(dim.Name)
					f := ir.FieldDef{
						Kind: ir.KindDimension,
						Name: cleanName,
					}
					if dim.Expr != nil {
						expr, err := t.translateExpr(dim.Expr)
						if err != nil {
							return ir.SourceDef{}, err
						}
						f.Expr = expr
					}
					src.Fields[cleanName] = f
				}
			} else if len(item.Measures) > 0 {
				for _, meas := range item.Measures {
					cleanName := stripBackticks(meas.Name)
					f := ir.FieldDef{
						Kind: ir.KindMeasure,
						Name: cleanName,
					}
					if meas.Expr != nil {
						expr, err := t.translateExpr(meas.Expr)
						if err != nil {
							return ir.SourceDef{}, err
						}
						f.Expr = expr
					}
					src.Fields[cleanName] = f
				}
			} else if len(item.JoinOnes) > 0 {
				for _, join := range item.JoinOnes {
					err := t.translateJoin(&src, join, ir.KindJoinOne)
					if err != nil {
						return ir.SourceDef{}, err
					}
				}
			} else if len(item.JoinManys) > 0 {
				for _, join := range item.JoinManys {
					err := t.translateJoin(&src, join, ir.KindJoinMany)
					if err != nil {
						return ir.SourceDef{}, err
					}
				}
			}
		}
	}

	// If fields are omitted, dynamically inject ir.FieldDef based on the database schema.
	if (decl.Body == nil || len(src.Fields) == 0) && decl.Def.Table != nil && t.db != nil && t.dialect != nil {
		schema, err := t.dialect.GetSchema(t.db, *decl.Def.Table)
		if err != nil {
			return ir.SourceDef{}, fmt.Errorf("schema introspection failed for table %s: %w", *decl.Def.Table, err)
		}
		for colName, colType := range schema {
			src.Fields[colName] = ir.FieldDef{
				Kind: ir.KindDimension,
				Name: colName,
				Type: colType,
			}
		}
	}

	return src, nil
}

func (t *Translator) translateJoin(src *ir.SourceDef, astJoin *Join, kind ir.FieldKind) error {
	cleanJoinName := stripBackticks(astJoin.Name)
	cleanJoinRef := stripBackticks(astJoin.Ref)
	joinSrc, ok := t.global.GetSource(cleanJoinRef)
	if !ok {
		return fmt.Errorf("join source not found: %s", cleanJoinRef)
	}
	f := ir.FieldDef{
		Kind:       kind,
		Name:       cleanJoinName,
		JoinSource: &joinSrc,
	}
	if astJoin.Expr != nil {
		onExpr, err := t.translateExpr(astJoin.Expr)
		if err != nil {
			return err
		}
		f.JoinOn = onExpr
	}
	src.Fields[cleanJoinName] = f
	return nil
}

func (t *Translator) translateExpr(e *Expr) (ir.Expr, error) {
	if e == nil {
		return nil, nil
	}
	return t.translateComparison(e.Comparison)
}

func (t *Translator) translateComparison(c *Comparison) (ir.Expr, error) {
	left, err := t.translateAddition(c.Left)
	if err != nil {
		return nil, err
	}
	for _, rhs := range c.Right {
		right, err := t.translateAddition(rhs.Right)
		if err != nil {
			return nil, err
		}
		left = ir.BinaryExpr{
			Left:  left,
			Op:    ir.BinaryOp(rhs.Op),
			Right: right,
		}
	}
	return left, nil
}

func (t *Translator) translateAddition(a *Addition) (ir.Expr, error) {
	left, err := t.translateMultiplication(a.Left)
	if err != nil {
		return nil, err
	}
	for _, rhs := range a.Right {
		right, err := t.translateMultiplication(rhs.Right)
		if err != nil {
			return nil, err
		}
		left = ir.BinaryExpr{
			Left:  left,
			Op:    ir.BinaryOp(rhs.Op),
			Right: right,
		}
	}
	return left, nil
}

func (t *Translator) translateMultiplication(m *Multiplication) (ir.Expr, error) {
	left, err := t.translateUnary(m.Left)
	if err != nil {
		return nil, err
	}
	for _, rhs := range m.Right {
		right, err := t.translateUnary(rhs.Right)
		if err != nil {
			return nil, err
		}
		left = ir.BinaryExpr{
			Left:  left,
			Op:    ir.BinaryOp(rhs.Op),
			Right: right,
		}
	}
	return left, nil
}

func (t *Translator) translateUnary(u *Unary) (ir.Expr, error) {
	expr, err := t.translateCast(u.Cast)
	if err != nil {
		return nil, err
	}
	if u.Op != "" {
		op := ir.UnaryOp(u.Op)
		if u.Op == "not" {
			op = ir.OpNot
		} else if u.Op == "-" {
			op = ir.OpNeg
		}
		return ir.UnaryExpr{Op: op, Child: expr}, nil
	}
	return expr, nil
}

func (t *Translator) translateCast(c *Cast) (ir.Expr, error) {
	expr, err := t.translatePrimary(c.Primary)
	if err != nil {
		return nil, err
	}
	for _, castType := range c.Casts {
		expr = ir.CastExpr{
			Expr: expr,
			Type: ir.DataType(castType),
		}
	}
	return expr, nil
}

func (t *Translator) translatePrimary(p *Primary) (ir.Expr, error) {
	if p.Number != nil {
		return ir.Literal{Value: *p.Number, Type: ir.TypeNumber}, nil
	}
	if p.String != nil {
		return ir.Literal{Value: *p.String, Type: ir.TypeString}, nil
	}
	if p.CastFunc != nil {
		expr, err := t.translateExpr(p.CastFunc.Expr)
		if err != nil {
			return nil, err
		}
		return ir.CastExpr{
			Expr: expr,
			Type: ir.DataType(p.CastFunc.Type),
		}, nil
	}
	if p.Field != nil {
		var cleanPath []string
		for _, part := range p.Field.Parts {
			cleanPath = append(cleanPath, stripBackticks(part))
		}
		return ir.FieldReference{Path: cleanPath}, nil
	}
	if p.SubExpr != nil {
		return t.translateExpr(p.SubExpr)
	}
	if p.Call != nil {
		var args []ir.Expr
		for _, arg := range p.Call.Args {
			translatedArg, err := t.translateExpr(arg)
			if err != nil {
				return nil, err
			}
			args = append(args, translatedArg)
		}
		return ir.CallExpr{Name: p.Call.Name, Args: args}, nil
	}
	return nil, fmt.Errorf("empty primary expression")
}

func stripBackticks(s string) string {
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		return s[1 : len(s)-1]
	}
	return s
}
