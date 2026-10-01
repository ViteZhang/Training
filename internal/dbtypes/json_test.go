package dbtypes

import (
	"encoding/json"
	"testing"
)

func TestNullJSON(t *testing.T) {
	var j NullJSON
	if err := j.Scan(nil); err != nil || j != nil {
		t.Fatal("NULL 应读成 nil")
	}
	if err := j.Scan([]byte(`[1]`)); err != nil || string(j) != "[1]" {
		t.Fatal(string(j))
	}
	if err := j.Scan(`{"a":1}`); err != nil || string(j) != `{"a":1}` {
		t.Fatal(string(j))
	}
	if j.Scan(1) == nil {
		t.Fatal("不支持的类型应报错")
	}
	if v, _ := NullJSON(nil).Value(); v != nil {
		t.Fatal("nil 应写 NULL")
	}
	if v, _ := NullJSON(`[]`).Value(); string(v.([]byte)) != "[]" {
		t.Fatal(v)
	}
	b, _ := json.Marshal(struct{ A, B NullJSON }{nil, NullJSON(`{"x":2}`)})
	if string(b) != `{"A":null,"B":{"x":2}}` {
		t.Fatal(string(b))
	}
	var s struct{ A, B NullJSON }
	if err := json.Unmarshal([]byte(`{"A":null,"B":[3]}`), &s); err != nil || s.A != nil || string(s.B) != "[3]" {
		t.Fatal(err, s)
	}
}
