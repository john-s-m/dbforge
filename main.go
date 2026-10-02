package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/term"
	yaml "gopkg.in/yaml.v3"
)

type DBForgeConfig struct {
	DBMS          string                `yaml:"DBMS"`
	DBUser        string                `yaml:"DBUser"`
	Package       string                `yaml:"Package"`
	OutputPath    string                `yaml:"OutputPath"`
	SecretManager *DBForgeSecretManager `yaml:"SecretManager"`
}

type DBForgeSecretManager struct {
	Type string `yaml:"Type"`
	Key  string `yaml:"Key"`
}

const DBForgeConfigFile = "./dbforge.config"

func writeFile(path string, filename string, content string) error {
	fullPath := filepath.Join(path, filename)

	return os.WriteFile(
		fullPath,
		[]byte(content),
		0644,
	)
}

func findDBForgeFiles(root string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(root, func(
		path string,
		d fs.DirEntry,
		err error,
	) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		if strings.EqualFold(filepath.Ext(path), ".dbf") {
			files = append(files, path)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("finding .dbf files: %w", err)
	}

	return files, nil
}

func promptPassword(prompt string) (string, error) {

	fmt.Print(prompt)

	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()

	if err != nil {
		return "", err
	}

	return string(password), nil
}

func connectDatabase(
	database string,
	dbUser string,
	password string,
) (*sql.DB, error) {

	dsn := fmt.Sprintf(
		"%s:%s@tcp(127.0.0.1:3306)/%s",
		dbUser,
		password,
		database,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf(
			"open database: %w",
			err,
		)
	}

	if err := db.Ping(); err != nil {
		db.Close()

		return nil, fmt.Errorf(
			"connect to database %s: %w",
			database,
			err,
		)
	}

	return db, nil
}

func replaceWhereClause(query string, nullClause string) (string, error) {
	re := regexp.MustCompile(`(?i)\bWHERE\b`)

	loc := re.FindStringIndex(query)
	if loc == nil {
		return "", fmt.Errorf("query does not contain a WHERE clause")
	}

	// Preserve everything through the WHERE keyword.
	prefix := query[:loc[1]]

	return prefix + " " + nullClause, nil
}

func addImportsFromDBType(imports map[string]bool, columns []DBColumns) error {
	for _, col := range columns {
		ltype, err := langTypeForDBType(col.Type)
		if err != nil {
			return err
		}
		imp, ok := langTypeImports[ltype]
		if !ok {
			continue
		}
		imports[imp] = true
	}
	return nil
}

func addImportsFromDBForgeType(imports map[string]bool, columns []DBForgeQueryArgument) error {
	for _, col := range columns {
		ltype, err := langType(col.Type)
		if err != nil {
			return err
		}
		imp, ok := langTypeImports[ltype]
		if !ok {
			continue
		}
		imports[imp] = true
	}
	return nil
}

func validateQueryResultColumns(queryName string, columns []DBColumns) error {

	seen := make(map[string]bool)

	for _, column := range columns {

		fieldName := goName(column.Name)

		if _, exists := seen[fieldName]; exists {
			return fmt.Errorf(
				"query %s has duplicate result field name %q; use an SQL alias",
				queryName,
				fieldName,
			)
		}

		seen[fieldName] = true
	}

	return nil
}

func validateQueryArguments(queryName string, arguments []DBForgeQueryArgument) error {

	seen := make(map[string]bool)

	for _, argument := range arguments {

		if argument.Name == "" {
			return fmt.Errorf(
				"query %s has an argument with no name",
				queryName,
			)
		}

		if _, exists := seen[argument.Name]; exists {
			return fmt.Errorf(
				"query %s has duplicate argument %q",
				queryName,
				argument.Name,
			)
		}

		seen[argument.Name] = true
	}

	return nil
}

func main() {

	var dbforgeConfig DBForgeConfig

	data, err := os.ReadFile(DBForgeConfigFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config File %s is missing or unreadable, it *MUST* be present:%v\n", DBForgeConfigFile, err)
		os.Exit(1)
	}

	if err := yaml.Unmarshal(data, &dbforgeConfig); err != nil {
		fmt.Fprintf(os.Stderr, "unable to parse DBForge Config file %s: %v\n", DBForgeConfigFile, err)
		os.Exit(1)
	}

	outputDir := flag.String("o", "", "output directory for generated Go code")

	flag.StringVar(outputDir, "output", "", "output directory for generated Go code")

	dbUser := flag.String("u", "", "database user account")

	flag.StringVar(dbUser, "user", "", "database user account")

	packageName := flag.String("p", "", "package for generated Go code")

	flag.StringVar(packageName, "package", "", "package for generated Go code")

	flag.Usage = func() {
		fmt.Fprintf(
			os.Stderr,
			"Usage: %s [options] [input-directory]\n\n",
			os.Args[0],
		)

		fmt.Fprintln(os.Stderr, "Options:")

		flag.PrintDefaults()

		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Examples:")

		fmt.Fprintln(os.Stderr, "  dbforge -u root -o ./generated ./db")

		fmt.Fprintln(os.Stderr, "  dbforge --user root --output ./generated ./db")
	}

	flag.Parse()

	if *dbUser != "" {
		dbforgeConfig.DBUser = *dbUser
	}

	if *outputDir != "" {
		dbforgeConfig.OutputPath = *outputDir
	}

	if *packageName != "" {
		dbforgeConfig.Package = *packageName
	}

	// The optional positional argument is the input directory.
	inputDir := "."

	if flag.NArg() > 0 {
		inputDir = flag.Arg(0)
	}

	// Make sure the input directory exists.
	info, err := os.Stat(inputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot access input directory %q: %v\n", inputDir, err)
		os.Exit(1)
	}

	if !info.IsDir() {
		fmt.Fprintf(os.Stderr, "error: %q is not a directory\n", inputDir)
		os.Exit(1)
	}

	if dbforgeConfig.DBUser == "" {
		fmt.Fprintln(os.Stderr, "DBUser is required in dbforge.config or via --user")
		os.Exit(1)
	}

	if dbforgeConfig.Package == "" {
		fmt.Fprintln(os.Stderr, "Package is required in dbforge.config")
		os.Exit(1)
	}

	// Make the output directory if necessary.
	if err := os.MkdirAll(dbforgeConfig.OutputPath, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot create output directory %q: %v\n", *outputDir, err)
		os.Exit(1)
	}

	password, pwderr := promptPassword(
		fmt.Sprintf("Password for database user %s: ", dbforgeConfig.DBUser),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading database password: %v\n", pwderr)
		os.Exit(1)
	}
	// Find all .dbf files.
	files, err := findDBForgeFiles(inputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if len(files) == 0 {
		fmt.Printf("No .dbf files found in %s\n", inputDir)
		return
	}

	fmt.Printf("Found %d DBForge file(s)\n", len(files))

	g := NewGenerator(dbforgeConfig)

	g.generateDBForgeExecutor(dbforgeConfig)
	writeFile(dbforgeConfig.OutputPath, "dbforgeexecutor.go", g.generatedCode.String())

	// Main DBForge processing loop.
	for _, filename := range files {
		fmt.Printf("\nProcessing %s\n", filename)

		dbf, err := Parse(filename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error parsing %s: %v\n", filename, err)
			continue
		}

		db, err := connectDatabase(dbf.Database, dbforgeConfig.DBUser, password)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error connecting to database: %v\n", err)
			continue
		}

		defer db.Close()

		for _, tbl := range dbf.Tables {
			g.resetState()
			columns, err := getTableColumns(db, dbf.Database, tbl.Table)
			if err != nil {
				fmt.Printf("**ERROR**  Failed to retrieve schema info/columns for Table: %s\n %v", tbl.Table, err)
				continue
			}

			imports := make(map[string]bool)

			err = addImportsFromDBType(imports, columns)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\nGeneration for table %s failed... Skipping...\n", err, tbl.Table)
				break
			}

			fmt.Fprintf(&g.generatedCode, `package %s

import (
	"context"
        "fmt"
	"database/sql"

	_ "github.com/go-sql-driver/mysql"
`,
				dbforgeConfig.Package)

			for imp := range imports {
				fmt.Fprintf(&g.generatedCode, `	"%s"
`,
					imp)
			}

			fmt.Fprintf(&g.generatedCode, ")\n\n")

			if err := g.generateTable(&tbl, columns); err != nil {
				fmt.Fprintf(os.Stderr, "**ERROR**  Failed to generate code for Table: %s\n %v", tbl.Table, err)
				continue
			}

			g.printWarnings()

			filename := fmt.Sprintf("%s.go", tbl.Table)
			if _, exists := g.usedFilenames[filename]; exists {
				fmt.Fprintf(os.Stderr,
					"***ERROR*** Duplicate filename, second instance in current run was not saved: %s",
					filename,
				)
				continue
			}

			g.usedFilenames[filename] = true

			if err := writeFile(dbforgeConfig.OutputPath, filename, g.generatedCode.String()); err != nil {
				fmt.Fprintf(os.Stderr, "**ERROR**  Failed write the generated code for Table: %s\n %v", tbl.Table, err)
				continue
			}
			fmt.Printf("%s\n", filename)
		}

		for _, qry := range dbf.Queries {
			g.resetState()
			err := validateQueryArguments(qry.QueryName, qry.Arguments)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\nGeneration for query %s failed... Skipping...\n", err, qry.QueryName)
				break
			}

			imports := make(map[string]bool)
			var columns []DBColumns

			if strings.ToUpper(qry.SchemaQuery) != "NORESULTS" {
				shortCircuitQuery := qry.SchemaQuery
				if qry.SchemaQuery == "" {
					scq, qerr := replaceWhereClause(qry.Query, qry.NullClause)
					if qerr != nil {
						fmt.Printf("**ERROR**  Failed to replace the where clause with the null clause for query: %s\n %v", qry.Query, qerr)
						continue
					}
					shortCircuitQuery = scq
				}

				columns, err = getQueryColumns(db, shortCircuitQuery)
				if err != nil {
					fmt.Printf("**ERROR**  Failed to retrieve schema info for Query: %s\n %v\nUsing Query:\n%s\n",
						qry.QueryName, err, shortCircuitQuery)
					continue
				}

				err = validateQueryResultColumns(qry.QueryName, columns)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%v\nGeneration for query %s failed... Skipping...\n", err, qry.QueryName)
					break
				}

				err = addImportsFromDBType(imports, columns)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%v\nGeneration for query %s failed... Skipping...\n", err, qry.QueryName)
					break
				}
			}

			err = addImportsFromDBForgeType(imports, qry.Arguments)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%v\nGeneration for query %s failed... Skipping...\n", err, qry.QueryName)
				break
			}

			fmt.Fprintf(&g.generatedCode, `package %s

import (
	"context"
`,
				dbforgeConfig.Package)

			for imp := range imports {
				fmt.Fprintf(&g.generatedCode, `	"%s"
`,
					imp)
			}

			fmt.Fprintf(&g.generatedCode, ")\n\n")

			// TODO: generateQuery
			if err := g.generateQuery(&qry, columns); err != nil {
				fmt.Fprintf(os.Stderr, "**ERROR**  Failed to generate code for Query: %s\n %v", qry.QueryName, err)
				continue
			}

			g.printWarnings()
			filename := fmt.Sprintf("%s.go", qry.QueryName)
			if _, exists := g.usedFilenames[filename]; exists {
				fmt.Fprintf(os.Stderr,
					"***ERROR*** Duplicate generated file in current run: %s",
					filename)
				continue
			}

			g.usedFilenames[filename] = true

			if err := writeFile(dbforgeConfig.OutputPath, filename, g.generatedCode.String()); err != nil {
				fmt.Fprintf(os.Stderr,
					"**ERROR**  Failed write the generated code for query: %s\n %v",
					qry.QueryName,
					err)
				continue
			}
			fmt.Printf("%s\n", filename)
		}
	}

	fmt.Println("\nDBForge complete.")
}
