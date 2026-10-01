// Package dbtypes 是 sqlc 生成代码用到的自定义列类型。
package dbtypes

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// NullJSON 是可为 NULL 的 JSON 列。json.RawMessage 不能接收 NULL（database/sql 不支持把 nil 扫进具名切片类型），
// 所以可空 JSON 列统一用它：NULL 读出来是 nil，写入 nil 存 NULL。
type NullJSON []byte

func (j *NullJSON) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append((*j)[:0], v...)
	case string:
		*j = NullJSON(v)
	default:
		return fmt.Errorf("dbtypes.NullJSON: 不支持的类型 %T", src)
	}
	return nil
}

func (j NullJSON) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return []byte(j), nil
}

func (j NullJSON) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("null"), nil
	}
	return json.RawMessage(j).MarshalJSON()
}

func (j *NullJSON) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*j = nil
		return nil
	}
	*j = append((*j)[:0], b...)
	return nil
}
