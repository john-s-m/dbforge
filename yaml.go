package main

import (
	"fmt"
	"os"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

/*
DBForge file
│
├── Tables
│    ├── ai_telemetry
│    ├── ai_request
│    └── ai_response
│
└── Queries
     ├── GetRecentTelemetry
     └── GetProviderStats

Sample YAML:
Database: telemetry
Package: telemetry

Tables:
  - Table: ai_telemetry
    Methods: all

  - Table: ai_request
    Methods:
      - select
      - insert
      - update

Queries:
  - QueryName: GetRecentTelemetry
    NullClause: "1 = 0"
    Arguments: requestId string, provider string
    Query: |
      SELECT
          *
      FROM ai_telemetry
      WHERE request_id = ?
        AND provider = ?

  - QueryName: GetProviderStats
    NullClause: "1 = 0"
    Arguments: provider string
    Query: |
      SELECT provider, COUNT(*)
      FROM ai_telemetry
      WHERE provider = ?
      GROUP BY provider
*/

var validMethods = map[string]bool{
	"select": true,
	"insert": true,
	"update": true,
	"delete": true,
}

type DBArgument struct {
	Name string
	Type string
}

type DBForge struct {
	Database string         `yaml:"Database"`
	Tables   []DBForgeTable `yaml:"Tables"`
	Queries  []DBForgeQuery `yaml:"Queries"`
}

type DBForgeTable struct {
	Table   string   `yaml:"Table"`
	Methods []string `yaml:"Methods"`
}

type DBForgeQuery struct {
	QueryName      string
	SchemaQuery    string
	NullClause     string
	Arguments      []DBForgeQueryArgument
	QueryArguments []DBForgeQueryArgument
	Query          string
}

type DBForgeQueryArgument struct {
	Name string
	Type DBForgeDataType
}

func (t *DBForgeTable) UnmarshalYAML(
	value *yaml.Node,
) error {

	var raw struct {
		Table   string `yaml:"Table"`
		Methods string `yaml:"Methods"`
	}

	if err := value.Decode(&raw); err != nil {
		return err
	}

	t.Table = raw.Table

	methods := strings.TrimSpace(raw.Methods)

	if methods == "" {
		t.Methods = nil
		return nil
	}

	parts := strings.Split(methods, ",")

	t.Methods = make([]string, 0, len(parts))

	for _, part := range parts {
		method := strings.TrimSpace(part)

		if method == "" {
			return fmt.Errorf(
				"table %q contains an empty method",
				t.Table,
			)
		}

		t.Methods = append(t.Methods, method)
	}

	return nil
}

func (q *DBForgeQuery) UnmarshalYAML(value *yaml.Node) error {

	var raw struct {
		QueryName   string `yaml:"QueryName"`
		SchemaQuery string `yaml:"SchemaQuery"`
		NullClause  string `yaml:"NullClause"`
		Arguments   string `yaml:"Arguments"`
		Query       string `yaml:"Query"`
	}

	if err := value.Decode(&raw); err != nil {
		return err
	}

	if strings.TrimSpace(raw.QueryName) == "" {
		return fmt.Errorf("QueryName is required")
	}

	sq := strings.TrimSpace(raw.SchemaQuery)
	nc := strings.TrimSpace(raw.NullClause)
	if sq == "" && nc == "" {
		return fmt.Errorf(
			"%s: SchemaQuery or NullClause is required",
			raw.QueryName,
		)
	}

	if strings.TrimSpace(raw.Query) == "" {
		return fmt.Errorf(
			"%s: Query is required",
			raw.QueryName,
		)
	}

	arguments, err := parseQueryArguments(raw.Arguments)
	if err != nil {
		return fmt.Errorf(
			"%s: %w",
			raw.QueryName,
			err,
		)
	}

	query, queryArguments, err := processQueryArguments(raw.Query, arguments)
	if err != nil {
		return fmt.Errorf(
			"%s: %w",
			raw.QueryName,
			err,
		)
	}

	q.QueryName = raw.QueryName
	q.NullClause = raw.NullClause
	q.SchemaQuery = raw.SchemaQuery
	q.Arguments = arguments
	q.QueryArguments = queryArguments
	q.Query = query

	return nil
}

func validateTables(tables []DBForgeTable) error {

	for i := range tables {

		table := &tables[i]

		if strings.TrimSpace(table.Table) == "" {
			return fmt.Errorf(
				"Tables[%d]: Table is required",
				i,
			)
		}

		if len(table.Methods) == 0 {
			return fmt.Errorf(
				"Tables[%d] (%s): Methods is required",
				i,
				table.Table,
			)
		}

		for j := range table.Methods {
			table.Methods[j] = strings.ToLower(
				strings.TrimSpace(table.Methods[j]),
			)
		}

		if containsMethod(table.Methods, "all") {
			table.Methods = []string{
				"select",
				"insert",
				"update",
				"delete",
			}
			continue
		}

		for _, method := range table.Methods {
			if !validMethods[method] {
				return fmt.Errorf(
					"Tables[%d] (%s): invalid method %q",
					i,
					table.Table,
					method,
				)
			}
		}
	}

	return nil
}

func parseQueryArguments(arguments string) ([]DBForgeQueryArgument, error) {

	arguments = strings.TrimSpace(arguments)

	if arguments == "" {
		return nil, nil
	}

	parts := strings.Split(arguments, ",")

	result := make([]DBForgeQueryArgument, 0, len(parts))

	seen := make(map[string]bool)

	for _, part := range parts {

		fields := strings.Fields(strings.TrimSpace(part))

		if len(fields) != 2 {
			return nil, fmt.Errorf(
				"invalid query argument %q: expected <name> <type>",
				part,
			)
		}

		name := fields[0]
		dataType := DBForgeDataType(fields[1])

		if seen[name] {
			return nil, fmt.Errorf(
				"duplicate query argument %q",
				name,
			)
		}

		seen[name] = true

		/*
			Use the language type map as the DBForge type
			validation mechanism.
		*/
		if _, ok := dbForgeToLangType[dataType]; !ok {
			return nil, fmt.Errorf(
				"invalid DBForge type %q for argument %q",
				dataType,
				name,
			)
		}

		result = append(
			result,
			DBForgeQueryArgument{
				Name: name,
				Type: dataType,
			},
		)
	}

	return result, nil
}

func isQueryArgumentCharacter(c byte) bool {
	return c == '_' ||
		c >= 'a' && c <= 'z' ||
		c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9'
}

func processQueryArguments(query string, declaredArguments []DBForgeQueryArgument) (string, []DBForgeQueryArgument, error) {

	/*
		Create a lookup table from the declared arguments.

		The slice is retained separately because its order is
		the order the developer specified in YAML.
	*/
	declared := make(map[string]DBForgeQueryArgument)

	for _, argument := range declaredArguments {
		declared[argument.Name] = argument
	}

	var result strings.Builder

	queryArguments := make(
		[]DBForgeQueryArgument,
		0,
	)

	for i := 0; i < len(query); {

		/*
			Single-quoted SQL string.
		*/
		if query[i] == '\'' {

			start := i
			i++

			for i < len(query) {

				if query[i] == '\'' {

					if i+1 < len(query) &&
						query[i+1] == '\'' {

						i += 2
						continue
					}

					i++
					break
				}

				i++
			}

			result.WriteString(query[start:i])
			continue
		}

		/*
			Double-quoted SQL string/identifier.
		*/
		if query[i] == '"' {

			start := i
			i++

			for i < len(query) {

				if query[i] == '"' {

					if i+1 < len(query) &&
						query[i+1] == '"' {

						i += 2
						continue
					}

					i++
					break
				}

				i++
			}

			result.WriteString(query[start:i])
			continue
		}

		/*
			MySQL quoted identifier.
		*/
		if query[i] == '`' {

			start := i
			i++

			for i < len(query) {

				if query[i] == '`' {

					if i+1 < len(query) &&
						query[i+1] == '`' {

						i += 2
						continue
					}

					i++
					break
				}

				i++
			}

			result.WriteString(query[start:i])
			continue
		}

		/*
			-- SQL comment
		*/
		if query[i] == '-' &&
			i+1 < len(query) &&
			query[i+1] == '-' {

			start := i
			i += 2

			for i < len(query) &&
				query[i] != '\n' {

				i++
			}

			result.WriteString(query[start:i])
			continue
		}

		/*
			# MySQL comment
		*/
		if query[i] == '#' {

			start := i
			i++

			for i < len(query) &&
				query[i] != '\n' {

				i++
			}

			result.WriteString(query[start:i])
			continue
		}

		/*
			SQL block comment
		*/

		if query[i] == '/' &&
			i+1 < len(query) &&
			query[i+1] == '*' {

			start := i
			i += 2

			for i+1 < len(query) &&
				!(query[i] == '*' &&
					query[i+1] == '/') {

				i++
			}

			if i+1 < len(query) {
				i += 2
			}

			result.WriteString(query[start:i])
			continue
		}

		/*
			DBForge substitution:

				${argument_name}
		*/
		if query[i] == '$' &&
			i+1 < len(query) &&
			query[i+1] == '{' {

			start := i
			i += 2

			nameStart := i

			for i < len(query) &&
				isQueryArgumentCharacter(query[i]) {

				i++
			}

			if i == nameStart {
				return "", nil, fmt.Errorf(
					"empty query argument substitution at position %d",
					start,
				)
			}

			if i >= len(query) ||
				query[i] != '}' {

				return "", nil, fmt.Errorf(
					"unterminated query argument substitution at position %d",
					start,
				)
			}

			name := query[nameStart:i]
			i++

			argument, ok := declared[name]

			if !ok {
				return "", nil, fmt.Errorf(
					"query references undeclared argument %q",
					name,
				)
			}

			/*
				This is deliberately a separate slice from
				declaredArguments.

				If the SQL contains:

					${provider}
					${request_id}
					${provider}

				then QueryArguments contains:

					provider
					request_id
					provider

				in precisely that order.
			*/
			queryArguments = append(
				queryArguments,
				argument,
			)

			result.WriteString("?")
			continue
		}

		result.WriteByte(query[i])
		i++
	}

	/*
		Every declared argument must actually occur in the query.
	*/
	used := make(map[string]bool)

	for _, argument := range queryArguments {
		used[argument.Name] = true
	}

	for _, argument := range declaredArguments {

		if !used[argument.Name] {
			return "", nil, fmt.Errorf(
				"argument %q is declared but not used by query",
				argument.Name,
			)
		}
	}

	return result.String(), queryArguments, nil
}

func validateQueries(queries []DBForgeQuery) error {

	seen := make(map[string]bool)

	for i := range queries {

		query := &queries[i]

		query.QueryName = strings.TrimSpace(query.QueryName)
		query.NullClause = strings.TrimSpace(query.NullClause)

		if query.QueryName == "" {
			return fmt.Errorf(
				"Queries[%d]: QueryName is required",
				i,
			)
		}

		if seen[query.QueryName] {
			return fmt.Errorf(
				"Queries[%d]: duplicate QueryName %q",
				i,
				query.QueryName,
			)
		}

		seen[query.QueryName] = true

		sq := strings.TrimSpace(query.SchemaQuery)
		nc := strings.TrimSpace(query.NullClause)
		if sq == "" && nc == "" {
			return fmt.Errorf(
				"Query %q has both SchemaQuery and NullClause as empty, at least one of them must be populated",
				query.QueryName,
			)
		}

		if strings.TrimSpace(query.Query) == "" {
			return fmt.Errorf(
				"Queries[%d] (%s): Query is required",
				i,
				query.QueryName,
			)
		}

		for _, arg := range query.Arguments {
			if arg.Name == "" {
				return fmt.Errorf(
					"Query %q contains an argument with no name",
					query.QueryName,
				)
			}

			if arg.Type == "" {
				return fmt.Errorf(
					"Query %q argument %q has no datatype",
					query.QueryName,
					arg.Name,
				)
			}
		}
	}

	return nil
}

func validateDBForge(dbf *DBForge) error {

	if strings.TrimSpace(dbf.Database) == "" {
		return fmt.Errorf("Database is required")
	}

	if len(dbf.Tables) == 0 && len(dbf.Queries) == 0 {
		return fmt.Errorf(
			"at least one Table or Query is required",
		)
	}

	if err := validateTables(dbf.Tables); err != nil {
		return err
	}

	if err := validateQueries(dbf.Queries); err != nil {
		return err
	}

	return nil
}

// Parse reads and validates a generator configuration file.
func Parse(filename string) (*DBForge, error) {

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var dbf DBForge

	if err := yaml.Unmarshal(data, &dbf); err != nil {
		return nil, fmt.Errorf(
			"unable to parse DBForge file %s: %w",
			filename,
			err,
		)
	}

	if err := validateDBForge(&dbf); err != nil {
		return nil, fmt.Errorf(
			"invalid DBForge file %s: %w",
			filename,
			err,
		)
	}

	return &dbf, nil
}

func containsMethod(methods []string, target string) bool {

	for _, method := range methods {
		if method == target {
			return true
		}
	}

	return false
}
