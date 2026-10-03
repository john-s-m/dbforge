# DBForge

DBForge is a Go code generator for database access code.

It uses a YAML definition to describe database tables and named SQL queries, then generates strongly typed Go structures and database access methods. The goal is to keep database access explicit, typed, and easy to inspect while eliminating repetitive SQL plumbing.

DBForge is deliberately code-generation based rather than reflection based. The generated code is ordinary Go code and can be reviewed, compiled, tested, and debugged like application code.

## Configuration

A DBForge configuration contains two kinds of definitions:

- **Tables** — generate standard database access methods for a table.
- **Queries** — generate strongly typed Go argument/result structures and an `Exec` method for arbitrary SQL.

Example:

```yaml
Database: telemetry

Tables:
  - Table: ai_telemetry
    Methods: all

  - Table: ai_request
    Methods: select, insert, update

Queries:
  - QueryName: GetRecentTelemetry
    NullClause: "1 = 0"
    Arguments: requestId string, provider string
    Query: |
      SELECT *
      FROM ai_telemetry
      WHERE request_id = ${requestId}
        AND provider = ${provider}

  - QueryName: GetProviderStats
    SchemaQuery: select provider, 1 from ai_telemetry where 1 = 2
    Arguments: provider string
    Query: |
      SELECT provider, COUNT(*)
      FROM ai_telemetry
      WHERE provider = ${provider}
      GROUP BY provider
```

`Database` is required, and a configuration must contain at least one table or query. The YAML is parsed and validated before generation. 

## Tables

A table definition identifies a database table and the operations DBForge should generate:

```yaml
Tables:
  - Table: ai_telemetry
    Methods: all
```

Supported methods are:

- `select`
- `insert`
- `update`
- `delete`
- `all`

`all` expands to all four standard methods. Method names are normalized to lowercase during validation. 

A subset may be specified:

```yaml
Tables:
  - Table: ai_request
    Methods: select, insert, update
```

This allows a table's generated API to expose only the operations the application needs.

### Generated table structures

For configured tables, DBForge generates the Go structures and methods required for the selected CRUD operations.

The generated table layer provides the standard database access patterns used by the application, including typed model structures and keyed `Get` methods where applicable. DBForge also generates the corresponding select/insert/update/delete access code rather than requiring the application to duplicate SQL and scan/bind logic.

The exact generated names are derived from the database schema and generator conventions; the generated source is the authoritative API for a particular schema.

## Named Queries

Named queries handle SQL that does not fit standard table CRUD operations.

```yaml
Queries:
  - QueryName: GetRecentTelemetry
    NullClause: "1 = 0"
    Arguments: requestId string, provider string
    Query: |
      SELECT *
      FROM ai_telemetry
      WHERE request_id = ${requestId}
        AND provider = ${provider}
```

A named query contains:

- `QueryName`
- `SchemaQuery` or `NullClause`
- optional `Arguments`
- `Query`

`QueryName` becomes the basis for the generated Go types and execution method.

`Query` is the SQL that is actually executed.

## Query arguments

Arguments use:

```text
<name> <type>, <name> <type>, ...
```

For example:

```yaml
Arguments: userId int32, roleId int32
```

SQL references arguments with:

```text
${argumentName}
```

For example:

```sql
WHERE user_id = ${userId}
  AND role_id = ${roleId}
```

DBForge converts these substitutions to SQL `?` parameters in the generated query and preserves their occurrence order. If an argument appears more than once, it appears more than once in the generated parameter list. 

Every declared argument must be used, and every `${...}` reference must correspond to a declared argument.

## Generated named-query structures

A named query generates a strongly typed argument structure, a result structure when the query returns results, and an `Exec` method.

Conceptually, a generated query has this form:

```go
type GetRecentTelemetryArguments struct {
    RequestId string
    Provider  string
}

type GetRecentTelemetryResults struct {
    // Columns returned by the query.
}

func (q *GetRecentTelemetryArguments) Exec(
    ctx context.Context,
    db DBForgeExecutor,
) ([]GetRecentTelemetryResults, error)
```

The exact fields and types are generated from the database result schema.

The important distinction is:

- `Arguments` describes values supplied by the caller.
- `${...}` references determine SQL parameter order.
- `SchemaQuery` or `NullClause` establishes the result-set schema.
- `Query` is the SQL actually executed.

This produces a strongly typed database API without requiring runtime reflection.

## SchemaQuery

`SchemaQuery` provides an explicit way to describe the result-set schema when `NullClause` is not sufficient.

For example:

```yaml
SchemaQuery: |
  SELECT provider, 1
  FROM ai_telemetry
  WHERE 1 = 2

Query: |
  SELECT provider, COUNT(*)
  FROM ai_telemetry
  WHERE provider = ${provider}
  GROUP BY provider
```

The schema query is used to establish the generated result structure. The `Query` is the SQL actually executed.

`SchemaQuery` is especially useful when the real query contains constructs that make a simple null-clause approach inadequate, including subqueries and more complicated result expressions.

## NullClause

`NullClause` is the simpler mechanism for describing a result schema when the query can be made to return no rows by adding a condition such as:

```yaml
NullClause: "1 = 0"
```

For example:

```yaml
NullClause: "1 = 0"

Query: |
  SELECT *
  FROM ai_telemetry
  WHERE request_id = ${requestId}
```

DBForge requires every named query to provide either `SchemaQuery` or `NullClause`.

Use `NullClause` when it is sufficient. Use `SchemaQuery` when the schema needs to be described independently, particularly for more complex SQL and subqueries.

## SchemaQuery and NoResults

`SchemaQuery` also supports the `NoResults` option.

`NoResults` declares that a named query does not return a result set. This is useful for command-style SQL such as:

- `INSERT`
- `UPDATE`
- `DELETE`

The `NoResults` tag is case-insensitive.

This allows the same named-query mechanism to represent both queries that return rows and commands that do not.

Conceptually:

```yaml
QueryName: DeleteUserPermission
SchemaQuery: |
  SELECT NoResults
Arguments: userId int32, permissionId int32
Query: |
  DELETE FROM user_permission
  WHERE user_id = ${userId}
    AND permission_id = ${permissionId}
```

The distinction is:

- a normal schema query describes the columns of a result set;
- `NoResults` declares that there is no result set;
- `Query` remains the SQL that DBForge executes.

## SQL parameter processing

DBForge does not perform a blind text replacement of `${...}`.

The query processor recognizes SQL regions that must not be interpreted as DBForge substitutions, including:

- single-quoted SQL strings
- double-quoted strings/identifiers
- MySQL backtick-quoted identifiers
- `--` comments
- `#` comments
- block comments

Only `${argument}` expressions outside those regions are processed.

Supported argument-name characters are letters, digits, and underscore. 

## Query validation

DBForge validates named queries before code generation.

It checks that:

- `QueryName` is present;
- `QueryName` is unique;
- `SchemaQuery` or `NullClause` is supplied;
- `Query` is present;
- every argument has a name;
- every argument has a datatype;
- every `${argument}` reference is declared;
- every declared argument is actually used.

Table definitions are also validated for required names, methods, and supported operations.

## Generated API philosophy

DBForge is intended to provide the advantages of generated database access while keeping SQL and generated code visible to the developer.

### SQL remains explicit

Application-specific SQL lives in the DBForge YAML rather than being hidden behind a runtime abstraction.

### Go remains strongly typed

Generated argument, model, and result structures provide compile-time types at the application boundary.

### No runtime reflection is required

The generated database code is ordinary Go code.

### Standard operations are generated

Repetitive CRUD, parameter binding, and result scanning code are generated rather than repeatedly handwritten.

### Complex SQL remains possible

Named queries are not restricted to simple lookups. `SchemaQuery` provides an explicit mechanism for describing the result shape of more complicated SQL.

## When to use each feature

Use a **table definition** for standard CRUD operations:

```yaml
Tables:
  - Table: user_permission
    Methods: all
```

Use a **named query** for application-specific SQL:

```yaml
Queries:
  - QueryName: GetUserPermissions
    ...
```

Use **`NullClause`** when the query's result schema can be established with a simple no-results condition.

Use **`SchemaQuery`** when the result schema needs to be described independently of the executable SQL, particularly for complex queries or subqueries.

Use **`SqhemaQuery: NoResults`** for named queries that execute commands and return no rows.

## Example

```yaml
Database: haleform

Tables:
  - Table: user
    Methods: select

  - Table: role
    Methods: select

  - Table: user_permission
    Methods: select, insert, delete

Queries:
  - QueryName: GetUserPermissions
    SchemaQuery: |
      SELECT
          permission_id,
          scope
      FROM user_permission
      WHERE 1 = 2
    Arguments: userId int32
    Query: |
      SELECT
          permission_id,
          scope
      FROM user_permission
      WHERE user_id = ${userId}

  - QueryName: DeleteUserPermission
    SchemaQuery: |
      SELECT NoResults
    Arguments: userId int32, permissionId int32
    Query: |
      DELETE FROM user_permission
      WHERE user_id = ${userId}
        AND permission_id = ${permissionId}
```

The resulting package exposes typed Go access to the configured tables and named queries.

## Design principles

DBForge follows several principles:

1. **Database access should be explicit.**
2. **SQL should remain visible to the developer.**
3. **Generated code should be ordinary, inspectable Go.**
4. **Types should be established at generation time rather than inferred dynamically at runtime.**
5. **Standard CRUD operations should not require repetitive handwritten code.**
6. **Complex application queries should remain possible without forcing them into a CRUD abstraction.**
7. **Generated APIs should follow consistent patterns across the application.**

DBForge is therefore a code-generation layer between the database schema/SQL and the application's Go service code.
