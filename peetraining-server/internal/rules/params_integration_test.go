package rules_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"peetraining-server/internal/logx"
	"peetraining-server/internal/rules"
	"peetraining-server/internal/store"
	"peetraining-server/internal/testenv"
)

// 迁移写入的 rule_params 初始值必须与 DefaultParams 一致，两处不能各说各话。
func TestSeededParamsMatchDefaults(t *testing.T) {
	env := testenv.New(t)
	ctx := context.Background()
	db, err := store.OpenMySQL(ctx, env.MySQLDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.MigrateUp(ctx, db, logx.New(io.Discard, slog.LevelInfo)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, "SELECT param_key, value FROM rule_params")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	m := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		m[k] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// 用空结构体解码（不从默认值出发），数据库里漏掉的字段也能被发现。
	all, _ := json.Marshal(m)
	var got rules.Params
	if err := json.Unmarshal(all, &got); err != nil {
		t.Fatal(err)
	}
	if want := rules.DefaultParams(); !reflect.DeepEqual(got, want) {
		t.Errorf("数据库里的规则参数与 DefaultParams 不一致：\n db   %+v\n code %+v", got, want)
	}
}
