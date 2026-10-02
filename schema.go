package main

import (
	"database/sql"
	"fmt"
)

type DBColumns struct {
	Name     string
	Type     string
	Length   int64
	Nullable bool
	Key      string
	Default  interface{}
	Extra    string
}

func getTableColumns(
	db *sql.DB,
	database string,
	table string,
) ([]DBColumns, error) {

	const query = `
            SELECT
                COLUMN_NAME,
                DATA_TYPE,
                COALESCE(CHARACTER_MAXIMUM_LENGTH, NUMERIC_PRECISION, 0),
                IS_NULLABLE,
                COLUMN_KEY,
                COLUMN_DEFAULT,
                EXTRA
            FROM INFORMATION_SCHEMA.COLUMNS
            WHERE TABLE_SCHEMA = ?
              AND TABLE_NAME = ?
            ORDER BY ORDINAL_POSITION
    	`

	rows, err := db.Query(query, database, table)
	if err != nil {
		return nil, fmt.Errorf(
			"query table schema: %w",
			err,
		)
	}
	defer rows.Close()

	columns := make([]DBColumns, 0)

	for rows.Next() {

		var column DBColumns
		var nullable string

		err := rows.Scan(
			&column.Name,
			&column.Type,
			&column.Length,
			&nullable,
			&column.Key,
			&column.Default,
			&column.Extra,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"scan table schema: %w",
				err,
			)
		}

		column.Nullable = nullable == "YES"

		columns = append(columns, column)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"reading table schema: %w",
			err,
		)
	}

	return columns, nil
}

func getQueryColumns(
	db *sql.DB,
	query string,
) ([]DBColumns, error) {

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
	}
	defer rows.Close()

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("get query column types: %w", err)
	}

	columns := make([]DBColumns, 0, len(columnTypes))

	for _, columnType := range columnTypes {

		column := DBColumns{
			Name: columnType.Name(),
		}

		// Database type, e.g. VARCHAR, BIGINT, DATETIME.
		column.Type = columnType.DatabaseTypeName()

		// Length is only meaningful for some types.
		if length, ok := columnType.Length(); ok {
			column.Length = int64(length)
		}

		// Nullable reports whether the database says the column
		// can contain NULL.
		if nullable, ok := columnType.Nullable(); ok {
			column.Nullable = nullable
		}

		columns = append(columns, column)
	}

	return columns, nil
}
