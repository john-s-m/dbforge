package main

import (
	"fmt"
	"strings"
)

var databaseToLangType = map[string]string{
	"tinyint":   "int8",
	"smallint":  "int16",
	"mediumint": "int32",
	"int":       "int32",
	"integer":   "int32",
	"bigint":    "int64",

	"tinyint unsigned":   "uint8",
	"smallint unsigned":  "uint16",
	"mediumint unsigned": "uint32",
	"int unsigned":       "uint32",
	"bigint unsigned":    "uint64",

	"float":  "float32",
	"double": "float64",

	"decimal": "decimal.Decimal",

	"char":       "string",
	"varchar":    "string",
	"text":       "string",
	"tinytext":   "string",
	"mediumtext": "string",
	"longtext":   "string",

	"binary":     "byte",
	"varbinary":  "[]byte",
	"blob":       "[]byte",
	"tinyblob":   "[]byte",
	"mediumblob": "[]byte",
	"longblob":   "[]byte",

	"date":      "time.Time",
	"time":      "time.Time",
	"datetime":  "time.Time",
	"timestamp": "time.Time",

	"json": "json.RawMessage",

	"uuid": "uuid.UUID",

	"enum": "string",
	"set":  "string",
}

func langTypeForDBType(dbType string) (string, error) {
	langType, ok := databaseToLangType[strings.ToLower(dbType)]
	if !ok {
		return "", fmt.Errorf(
			"unsupported MySQL data type: %s",
			dbType,
		)
	}

	return langType, nil
}

type DBForgeDataType string

const (
	DBForgeTypeInt8  DBForgeDataType = "int8"
	DBForgeTypeInt16 DBForgeDataType = "int16"
	DBForgeTypeInt32 DBForgeDataType = "int32"
	DBForgeTypeInt64 DBForgeDataType = "int64"

	DBForgeTypeUint8  DBForgeDataType = "uint8"
	DBForgeTypeUint16 DBForgeDataType = "uint16"
	DBForgeTypeUint32 DBForgeDataType = "uint32"
	DBForgeTypeUint64 DBForgeDataType = "uint64"

	DBForgeTypeByte DBForgeDataType = "byte"
	DBForgeTypeBool DBForgeDataType = "bool"

	DBForgeTypeFloat32 DBForgeDataType = "float32"
	DBForgeTypeFloat64 DBForgeDataType = "float64"
	DBForgeTypeDecimal DBForgeDataType = "decimal"

	DBForgeTypeString DBForgeDataType = "string"
	DBForgeTypeBytes  DBForgeDataType = "bytes"
	DBForgeTypeJSON   DBForgeDataType = "json"

	DBForgeTypeDate      DBForgeDataType = "date"
	DBForgeTypeTime      DBForgeDataType = "time"
	DBForgeTypeDateTime  DBForgeDataType = "datetime"
	DBForgeTypeTimestamp DBForgeDataType = "timestamp"

	DBForgeTypeUUID DBForgeDataType = "uuid"

	DBForgeTypeEnum DBForgeDataType = "enum"
	DBForgeTypeSet  DBForgeDataType = "set"
)

var dbForgeToLangType = map[DBForgeDataType]string{
	DBForgeTypeInt8:  "int8",
	DBForgeTypeInt16: "int16",
	DBForgeTypeInt32: "int32",
	DBForgeTypeInt64: "int64",

	DBForgeTypeUint8:  "uint8",
	DBForgeTypeUint16: "uint16",
	DBForgeTypeUint32: "uint32",
	DBForgeTypeUint64: "uint64",

	DBForgeTypeByte: "byte",
	DBForgeTypeBool: "bool",

	DBForgeTypeFloat32: "float32",
	DBForgeTypeFloat64: "float64",
	DBForgeTypeDecimal: "decimal.Decimal",

	DBForgeTypeString: "string",
	DBForgeTypeBytes:  "[]byte",
	DBForgeTypeJSON:   "json.RawMessage",

	DBForgeTypeDate:      "time.Time",
	DBForgeTypeTime:      "time.Time",
	DBForgeTypeDateTime:  "time.Time",
	DBForgeTypeTimestamp: "time.Time",

	DBForgeTypeUUID: "uuid.UUID",

	DBForgeTypeEnum: "string",
	DBForgeTypeSet:  "string",
}

func langType(dataType DBForgeDataType) (string, error) {
	langType, ok := dbForgeToLangType[dataType]
	if !ok {
		return "", fmt.Errorf(
			"unsupported DBForge data type %q",
			dataType,
		)
	}

	return langType, nil
}

var langTypeImports = map[string]string{
	"time.Time":       "time",
	"json.RawMessage": "encoding/json",
	"decimal.Decimal": "github.com/shopspring/decimal",
}
