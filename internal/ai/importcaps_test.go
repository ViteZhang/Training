package ai

import (
	"errors"
	"testing"
)

// 块上没有年份、分值时，模型会把可选字段输出成 null（qwen-plus 实测），应按缺省处理而不是整批判为不合格。
func TestStructureAcceptsNullOptionals(t *testing.T) {
	in := StructureIn{Blocks: []Block{{ID: 1, Page: 1, Text: "1. 现代汉民族共同语"}}}
	out, err := Structure.validate(in, `{"items":[{"block":1,"kind":"question","qtype":"term","stem":"现代汉民族共同语",
		"options":null,"answer":null,"score":null,"exam_year":null,"question_no":"1","page":1,"confidence":0.9,"is_recollection":null}]}`)
	if err != nil {
		t.Fatalf("可选字段为 null 应能通过校验：%v", err)
	}
	it := out.Items[0]
	if it.ExamYear != nil || it.Score != nil || it.Answer != "" || it.Options != nil {
		t.Fatalf("null 应解析为缺省：%+v", it)
	}
	// 必填字段仍然要有：缺题型照样不合格。
	_, err = Structure.validate(in, `{"items":[{"block":1,"kind":"question","qtype":null,"stem":"现代汉民族共同语","page":1,"confidence":0.9}]}`)
	if !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("题目缺题型应不合格：%v", err)
	}
	// 范围校验不受影响。
	_, err = Structure.validate(in, `{"items":[{"block":1,"kind":"question","qtype":"term","stem":"现代汉民族共同语","exam_year":1900,"page":1,"confidence":0.9}]}`)
	if !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("年份超出范围应不合格：%v", err)
	}
}
