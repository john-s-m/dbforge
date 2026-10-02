package main

import (
	"fmt"
	"os"
	"strings"
	"unicode"
)

type DiagnosticLevel int

const (
	DiagnosticWarning DiagnosticLevel = iota
	DiagnosticError
)

type Diagnostic struct {
	Level   DiagnosticLevel
	Table   string
	Message string
}

type Generator struct {
	config            DBForgeConfig
	generatedCode     strings.Builder
	diagnostics       []Diagnostic
	usedLanguageNames map[string]bool
	usedFilenames     map[string]bool
}

func NewGenerator(config DBForgeConfig) *Generator {
	return &Generator{
		config:            config,
		usedLanguageNames: make(map[string]bool),
		diagnostics:       make([]Diagnostic, 0),
		usedFilenames:     make(map[string]bool),
	}
}

func (g *Generator) resetState() {
	g.generatedCode.Reset()
	g.diagnostics = nil
}

func (g *Generator) addWarning(table, message string) {
	g.diagnostics = append(g.diagnostics, Diagnostic{
		Level:   DiagnosticWarning,
		Table:   table,
		Message: message,
	})
}

func (g *Generator) printWarnings() {
	for _, d := range g.diagnostics {
		fmt.Fprintf(os.Stderr, "%s\n", d.Message)
	}
}

func goName(name string) string {
	var result strings.Builder

	capitalize := true

	for _, r := range name {
		if r == '_' {
			capitalize = true
			continue
		}

		if capitalize {
			result.WriteRune(unicode.ToUpper(r))
			capitalize = false
		} else {
			result.WriteRune(r)
		}
	}

	return result.String()
}

func isGeneratedColumn(column DBColumns) bool {
	extra := strings.ToLower(column.Extra)

	return strings.Contains(extra, "auto_increment") ||
		strings.Contains(extra, "generated")
}

func primaryKeyColumns(columns []DBColumns) []DBColumns {
	var result []DBColumns

	for _, column := range columns {
		if column.Key == "PRI" {
			result = append(result, column)
		}
	}

	return result
}

func nonPrimaryKeyColumns(columns []DBColumns) []DBColumns {
	var result []DBColumns

	for _, column := range columns {
		if column.Key != "PRI" {
			result = append(result, column)
		}
	}

	return result
}

func insertColumns(columns []DBColumns) []DBColumns {
	var result []DBColumns

	for _, column := range columns {
		if isGeneratedColumn(column) {
			continue
		}

		if column.Default != nil {
			continue
		}

		result = append(result, column)
	}

	return result
}

func updateColumns(columns []DBColumns) []DBColumns {
	var result []DBColumns

	for _, column := range columns {
		if column.Key == "PRI" {
			continue
		}

		if isGeneratedColumn(column) {
			continue
		}

		result = append(result, column)
	}

	return result
}

func generateSQLColumns(
	b *strings.Builder,
	columns []DBColumns,
	separator string,
	suffix string,
) {
	sep := ""
	for _, column := range columns {
		fmt.Fprintf(
			b,
			"%s%s%s",
			sep,
			column.Name,
			suffix,
		)
		sep = separator
	}
}

func generateGoColumns(
	b *strings.Builder,
	columns []DBColumns,
	prefix string,
) {
	comma := ""

	for _, column := range columns {
		fmt.Fprintf(
			b,
			"%s\t%s%s",
			comma,
			prefix,
			goName(column.Name),
		)

		comma = ",\n"
	}
}

func (g *Generator) generateDBForgeExecutor(config DBForgeConfig) {

	fmt.Fprintf(&g.generatedCode, `package %s

import (
	"context"
	"database/sql"

	_ "github.com/go-sql-driver/mysql"
)

type DBForgeExecutor interface {
	ExecContext(
		ctx context.Context,
		query string,
		args ...any,
	) (sql.Result, error)

	QueryContext(
		ctx context.Context,
		query string,
		args ...any,
	) (*sql.Rows, error)
}

type DBForgeDatabase interface {
	DBForgeExecutor

	BeginTx(
		ctx context.Context,
		opts *sql.TxOptions,
	) (*sql.Tx, error)
}
`,
		config.Package)
}

func (g *Generator) generateGetter(goTableName string, colName string, colType string) {
	b := &g.generatedCode

	fmt.Fprintf(b, `func (t *%s) Get%s() %s {
	return (t.%s)
}

`,
		goTableName,
		colName,
		colType,
		colName,
	)
}

func (g *Generator) generateSetter(goTableName string, colName string, colType string) {
	b := &g.generatedCode

	fmt.Fprintf(b, `func (t *%s) Set%s( arg %s ) {
	t.%s = arg
}

`,
		goTableName,
		colName,
		colType,
		colName,
	)
}

func (g *Generator) generateTableStruct(tbl *DBForgeTable, columns []DBColumns) error {

	b := &g.generatedCode
	goTableName := goName(tbl.Table)
	if _, exists := g.usedLanguageNames[goTableName]; exists {
		return fmt.Errorf("***ERROR*** Duplicate language object generated: %s", goTableName)
	}
	g.usedLanguageNames[goTableName] = true

	fmt.Fprintf(b, "type %s struct {\n", goTableName)

	for _, column := range columns {

		langType, err := langTypeForDBType(strings.ToUpper(column.Type))
		pointer := ""
		if column.Nullable {
			pointer = "*"
		}

		if err != nil {
			return fmt.Errorf(
				"***ERROR*** No Language type mapping for database type %s on column %s",
				column.Type,
				column.Name,
			)
		}

		fmt.Fprintf(
			b,
			"\t%s %s%s\n",
			goName(column.Name),
			pointer,
			langType,
		)
	}

	b.WriteString("}\n\n")

	// generate Getters & Setters

	for _, column := range columns {

		langType, _ := langTypeForDBType(strings.ToUpper(column.Type))
		if column.Nullable {
			langType = "*" + langType
		}
		colName := goName(column.Name)
		g.generateGetter(goTableName, colName, langType)
		g.generateSetter(goTableName, colName, langType)
	}

	return nil
}

func (g *Generator) generateSelectMethod(tbl *DBForgeTable, columns []DBColumns) error {

	b := &g.generatedCode
	pkColumns := primaryKeyColumns(columns)

	if len(pkColumns) == 0 {
		g.addWarning(tbl.Table, "WARNING: Cannot generate a SELECT, table has no primary key")
		return nil
	}

	var sql strings.Builder

	sql.WriteString("\tSELECT ")

	generateSQLColumns(&sql, columns, ",\n\t\t", "")

	fmt.Fprintf(
		&sql,
		`
	FROM %s
	WHERE `,
		tbl.Table,
	)

	generateSQLColumns(&sql, pkColumns, "\n\t\tAND ", " = ?")

	goTableName := goName(tbl.Table)
	fmt.Fprintf(b, `
func (t *%s) Select(
	ctx context.Context,
	db DBForgeExecutor,
) ([]%s, error) {

	const sqlQuery = `+"`"+`
%s`+"`"+`

	rows, err := db.QueryContext(
		ctx,
		sqlQuery,
`,
		goTableName,
		goTableName,
		sql.String(),
	)

	// Generate PK arguments...
	generateGoColumns(b, pkColumns, "\tt.")

	// Close out QueryContext and Generate Scan...
	fmt.Fprintf(b, `)
        if err != nil {
                return nil, err
        }
        defer rows.Close()

        var returnRows []%s
        for rows.Next() {
                var currentRow %s

                err = rows.Scan( 
`, goTableName, goTableName)

	generateGoColumns(b, columns, "\t\t&currentRow.")

	fmt.Fprintf(b, `	)
	        if err != nil {
        	        return nil, err
	        }

                returnRows = append(returnRows, currentRow)
        }

        if err = rows.Err(); err != nil {
		        return nil, err
        }

        return returnRows, nil
}

`)

	return nil
}

func (g *Generator) generateEndOfMethod(andSelect bool, isDelete bool) error {

	b := &g.generatedCode
	rval := "0"
	if andSelect {
		rval = "nil"
	}

	fmt.Fprintf(
		b,
		`
	if err != nil {
		return %s, err
	}
`,
		rval,
	)

	if !andSelect {
		fmt.Fprintf(
			b,
			`
	var rows int64
	rows, err = result.RowsAffected()
	if err != nil {
		return %s, err
	}
`,
			rval,
		)
	} else if !isDelete {
		fmt.Fprintf(b, `	returnRows, err := t.Select(ctx, localdb)
	if err != nil {
		return nil, err
	}
`)
	}

	fmt.Fprintf(b, `
	if tx != nil {	
		if err := tx.Commit(); err != nil {
			return %s, err
		}
	}
`,
		rval)

	rval = "rows"
	if andSelect {
		rval = "returnRows"
	}

	fmt.Fprintf(b, `
	return %s, nil
}
`,
		rval)

	return nil
}

func (g *Generator) generateInsertMethod(tbl *DBForgeTable, columns []DBColumns, andSelect bool) error {

	b := &g.generatedCode
	insertCols := insertColumns(columns)
	pkColumns := primaryKeyColumns(columns)

	if len(pkColumns) == 0 {
		g.addWarning(tbl.Table, "WARNING: INSERT statement generated, but table has no primary key")
	}

	goTableName := goName(tbl.Table)
	rtype := "int64"
	andSel := ""
	rval := "0"
	useResult := "result"
	if andSelect {
		if !(len(pkColumns) == 1 &&
			strings.Contains(strings.ToLower(pkColumns[0].Extra), "auto_increment")) {

			useResult = "_"
		}
		andSel = "AndSelect"
		rval = "nil"
		rtype = fmt.Sprintf("[]%s", goTableName)
	}

	// Generate INSERT SQL.
	var sql strings.Builder

	fmt.Fprintf(
		&sql,
		"\tINSERT INTO %s ( ",
		tbl.Table,
	)

	generateSQLColumns(&sql, insertCols, ",\n\t\t", "")

	sql.WriteString("\n\t)\n\tVALUES (")

	comma := ""
	for range insertCols {
		fmt.Fprintf(&sql, "%s\n\t\t?", comma)
		comma = ","
	}

	sql.WriteString("\n\t)")

	fmt.Fprintf(
		b,
		`
func (t *%s) Insert%s(
	ctx context.Context,
	db DBForgeExecutor,
        execInTransaction bool,
) (%s, error) {

	const sqlQuery = `+"`"+`
%s`+"`"+`

	localdb := db
	var tx *sql.Tx

	if execInTransaction {
		realDb, ok := db.(DBForgeDatabase)
		if !ok {
			return %s, fmt.Errorf(
				"Database executor does not support starting a transaction",
			)
		}

		txl, err := realDb.BeginTx(ctx, nil)
		if err != nil {
			return %s, err
		}
		tx = txl
		localdb = tx
		defer tx.Rollback()
	}

	%s, err := localdb.ExecContext(
		ctx,
		sqlQuery,
`,
		goTableName,
		andSel,
		rtype,
		sql.String(),
		rval,
		rval,
		useResult,
	)

	// INSERT arguments.
	generateGoColumns(b, insertCols, "\tt.")

	fmt.Fprintf(b, `,
	)
	if err != nil {
		return %s, err
	}

`, rval)

	// Populate an auto-generated single-column primary key.
	if len(pkColumns) == 1 &&
		strings.Contains(
			strings.ToLower(pkColumns[0].Extra),
			"auto_increment",
		) {

		// We don't need to check the type, it was already validated in generateTableStruct()
		ltype, _ := langTypeForDBType(pkColumns[0].Type)
		fmt.Fprintf(
			b,
			`	if tempID, err := result.LastInsertId(); err == nil {
		t.%s = %s(tempID)
	} else {
		return %s, err
	}
`,
			goName(pkColumns[0].Name),
			ltype,
			rval,
		)
	}

	g.generateEndOfMethod(andSelect, false)
	return nil
}

func (g *Generator) generateUpdateMethod(tbl *DBForgeTable, columns []DBColumns, andSelect bool) error {

	b := &g.generatedCode
	pkColumns := primaryKeyColumns(columns)

	if len(pkColumns) == 0 {
		g.addWarning(tbl.Table, "WARNING: Cannot generate an UPDATE, table has no primary key")
		return nil
	}

	updateCols := updateColumns(columns)

	if len(updateCols) == 0 {
		g.addWarning(tbl.Table, "WARNING: Cannot generate an UPDATE, table has no updateable columns")
		return nil
	}

	goTableName := goName(tbl.Table)
	rtype := "int64"
	andSel := ""
	rval := "0"
	useResult := "result"
	if andSelect {
		useResult = "_"
		andSel = "AndSelect"
		rval = "nil"
		rtype = fmt.Sprintf("[]%s", goTableName)
	}

	// Generate UPDATE SQL.
	var sql strings.Builder

	fmt.Fprintf(
		&sql,
		"\tUPDATE %s\n\tSET ",
		tbl.Table,
	)

	generateSQLColumns(&sql, updateCols, ",\n\t\t", " = ?")
	sql.WriteString("\n\tWHERE ")

	generateSQLColumns(&sql, pkColumns, "\n\t\tAND ", " = ?")

	fmt.Fprintf(
		b,
		`
func (t *%s) Update%s(
	ctx context.Context,
	db DBForgeExecutor,
        execInTransaction bool,
) (%s, error) {

	const sqlQuery = `+"`"+`
%s`+"`"+`

	localdb := db
	var tx *sql.Tx

	if execInTransaction {
		realDb, ok := db.(DBForgeDatabase)
		if !ok {
			return %s, fmt.Errorf(
				"Database executor does not support starting a transaction",
			)
		}

		txl, err := realDb.BeginTx(ctx, nil)
		if err != nil {
			return %s, err
		}
		tx = txl
		localdb = tx
		defer tx.Rollback()
	}


	%s, err := localdb.ExecContext(
		ctx,
		sqlQuery,
`,
		goTableName,
		andSel,
		rtype,
		sql.String(),
		rval,
		rval,
		useResult,
	)

	// UPDATE arguments.
	generateGoColumns(b, updateCols, "\tt.")

	b.WriteString(",\n")

	// Primary-key arguments.
	generateGoColumns(b, pkColumns, "\tt.")

	fmt.Fprintf(b, ",\n\t\t)\n")
	g.generateEndOfMethod(andSelect, false)
	return nil
}

func (g *Generator) generateDeleteMethod(tbl *DBForgeTable, columns []DBColumns, andSelect bool) error {

	pkColumns := primaryKeyColumns(columns)
	b := &g.generatedCode

	if len(pkColumns) == 0 {
		g.addWarning(tbl.Table, "WARNING: Cannot generate s DELETE, table has no primary key")
		return nil
	}

	goTableName := goName(tbl.Table)
	rtype := "int64"
	andSel := ""
	rval := "0"
	useResult := "result"
	if andSelect {
		useResult = "_"
		andSel = "AndSelect"
		rval = "nil"
		rtype = fmt.Sprintf("[]%s", goTableName)
	}

	// Generate DELETE SQL.
	var sql strings.Builder

	fmt.Fprintf(
		&sql,
		"\tDELETE FROM %s\n\tWHERE ",
		tbl.Table,
	)

	generateSQLColumns(&sql, pkColumns, "\n\t\tAND ", " = ?")

	fmt.Fprintf(
		b,
		`
func (t *%s) Delete%s(
	ctx context.Context,
	db DBForgeExecutor,
        execInTransaction bool,
) (%s, error) {

	localdb := db
	var tx *sql.Tx

	if execInTransaction {
		realDb, ok := db.(DBForgeDatabase)
		if !ok {
			return %s, fmt.Errorf(
				"Database executor does not support starting a transaction",
			)
		}

		txl, err := realDb.BeginTx(ctx, nil)
		if err != nil {
			return %s, err
		}
		tx = txl
		localdb = tx
		defer tx.Rollback()
	}

`,
		goTableName,
		andSel,
		rtype,
		rval,
		rval,
	)

	if andSelect {
		fmt.Fprint(b, `
	returnRows, selErr := t.Select(ctx, localdb)
	if selErr != nil {
		return nil, selErr
	}
`)
	}

	fmt.Fprintf(
		b,
		`	const sqlQuery = `+"`"+`
%s`+"`"+`

	%s, err := localdb.ExecContext(
		ctx,
		sqlQuery,
`,
		sql.String(),
		useResult,
	)

	// DELETE arguments.
	generateGoColumns(b, pkColumns, "t.")

	fmt.Fprintf(b, ",\n\t)\n\n")
	g.generateEndOfMethod(andSelect, true)

	return nil
}

func (g *Generator) generateTable(tbl *DBForgeTable, columns []DBColumns) error {

	pkColumns := primaryKeyColumns(columns)

	if len(pkColumns) == 0 {
		return fmt.Errorf(
			"table %s has no primary key",
			tbl.Table,
		)
	}

	if err := g.generateTableStruct(tbl, columns); err != nil {
		return err
	}

	if err := g.generateSelectMethod(tbl, columns); err != nil {
		return err
	}

	if err := g.generateInsertMethod(tbl, columns, false); err != nil {
		return err
	}

	if err := g.generateInsertMethod(tbl, columns, true); err != nil {
		return err
	}

	if err := g.generateUpdateMethod(tbl, columns, false); err != nil {
		return err
	}

	if err := g.generateUpdateMethod(tbl, columns, true); err != nil {
		return err
	}

	if err := g.generateDeleteMethod(tbl, columns, false); err != nil {
		return err
	}

	if err := g.generateDeleteMethod(tbl, columns, true); err != nil {
		return err
	}

	return nil
}

func (g *Generator) generateQuery(query *DBForgeQuery, columns []DBColumns) error {

	b := &g.generatedCode
	if query.QueryName == "" {
		return fmt.Errorf("query name is required")
	}

	// ------------------------------------------------------------
	// Arguments structure
	// ------------------------------------------------------------

	argName := fmt.Sprintf("%sArguments", query.QueryName)
	rsltName := fmt.Sprintf("%sResults", query.QueryName)
	if _, exists := g.usedLanguageNames[argName]; exists {
		return fmt.Errorf("***ERROR*** Duplicate language object generated: %s", argName)
	}
	g.usedLanguageNames[argName] = true

	if _, exists := g.usedLanguageNames[rsltName]; exists {
		return fmt.Errorf("***ERROR*** Duplicate language object generated: %s", rsltName)
	}
	g.usedLanguageNames[rsltName] = true

	fmt.Fprintf(b, "type %s struct {\n", argName)

	for _, argument := range query.Arguments {

		langType, err := langType(argument.Type)
		if err != nil {
			return fmt.Errorf(
				"query %s: unsupported argument datatype %q",
				query.QueryName,
				argument.Type,
			)
		}

		fmt.Fprintf(b, "\t%s *%s\n", goName(argument.Name), langType)
	}

	b.WriteString("}\n\n")

	for _, argument := range query.Arguments {
		langType, _ := langType(argument.Type)
		fmt.Fprintf(b, "func (t *%s)Set%s( %s *%s ) {\n\tt.%s = %s\n}\n\n",
			argName, goName(argument.Name), goName(argument.Name), langType, goName(argument.Name), goName(argument.Name))
	}

	// ------------------------------------------------------------
	// Results structure
	// ------------------------------------------------------------

	rval := "0"
	rtype := "int64"
	executionMethod := "result, err := db.ExecContext"
	if len(columns) > 0 {
		rval = "nil"
		rtype = fmt.Sprintf("[]%s", rsltName)
		executionMethod = "rows, err := db.QueryContext"

		fmt.Fprintf(b, "type %s struct {\n", rsltName)

		for _, column := range columns {

			langType, err := langTypeForDBType(strings.ToUpper(column.Type))
			if err != nil {
				return fmt.Errorf(
					"query %s: unsupported result datatype %q for column %s",
					query.QueryName,
					column.Type,
					column.Name,
				)
			}

			if column.Nullable {
				langType = "*" + langType
			}
			fmt.Fprintf(b, "\t%s %s\n", goName(column.Name), langType)
		}

		b.WriteString("}\n\n")

		for _, column := range columns {
			langType, _ := langTypeForDBType(strings.ToUpper(column.Type))
			if column.Nullable {
				langType = "*" + langType
			}
			fmt.Fprintf(b, "func (t *%s)Get%s() %s {\n\treturn t.%s\n}\n\n",
				rsltName, goName(column.Name), langType, goName(column.Name))
		}
	}

	// ------------------------------------------------------------
	// Exec method
	// ------------------------------------------------------------

	fmt.Fprintf(
		b,
		`func (q *%sArguments) Exec(
	ctx context.Context,
	db DBForgeExecutor,
 ) (%s, error) {

	const sql = `,
		query.QueryName,
		rtype,
	)
	fmt.Fprintf(b, "`\n%s`\n", query.Query)
	fmt.Fprintf(
		b,
		`
	%s(
		ctx,
		sql,
`, executionMethod,
	)

	// ------------------------------------------------------------
	// Query arguments
	// ------------------------------------------------------------

	for _, argument := range query.QueryArguments {
		fmt.Fprintf(b, "\t\tq.%s,\n", goName(argument.Name))
	}

	fmt.Fprintf(b, `	)

`)
	if len(columns) > 0 {
		fmt.Fprintf(b,
			`	if err != nil { // xyz
		return %s, err
	}
	defer rows.Close()

	var returnRows %s

`, rval, rtype)

		fmt.Fprintf(b,
			`	for rows.Next() {
		var currentRow %s

		err = rows.Scan(
`,
			rsltName)

		// ------------------------------------------------------------
		// Scan columns
		// ------------------------------------------------------------

		for _, column := range columns {
			fmt.Fprintf(b, "\t\t\t&currentRow.%s,\n", goName(column.Name))
		}

		b.WriteString(`		)
		if err != nil {
			return nil, err
		}

		returnRows = append(returnRows, currentRow)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return returnRows, nil
`)
	} else {
		fmt.Fprintf(b,
			`	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
`)
	}
	b.WriteString(`}

`)

	return nil
}
