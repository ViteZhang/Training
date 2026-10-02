package practice

import "testing"

func TestJudge(t *testing.T) {
	tf := []Option{{"A", "正确"}, {"B", "错误"}}
	cases := []struct {
		name, qtype, answer, text string
		options                   []Option
		selected                  []string
		correct, ok               bool
	}{
		{"单选对", "single_choice", "A", "", nil, []string{"A"}, true, true},
		{"单选错", "single_choice", "A", "", nil, []string{"B"}, false, true},
		{"多选顺序无关", "multi_choice", "A、C", "", nil, []string{"C", "A"}, true, true},
		{"多选少选算错", "multi_choice", "AC", "", nil, []string{"A"}, false, true},
		{"判断按选项文字", "true_false", "对", "", tf, []string{"A"}, true, true},
		{"判断答案是字母", "true_false", "B", "", tf, []string{"B"}, true, true},
		{"判断错", "true_false", "正确", "", tf, []string{"B"}, false, true},
		{"填空忽略标点空格", "fill_blank", "刘勰；文心雕龙", "刘勰;《文心雕龙》", nil, nil, true, true},
		{"填空空数不对", "fill_blank", "刘勰；文心雕龙", "刘勰", nil, nil, false, true},
		{"没有答案不能判", "single_choice", "", "", nil, []string{"A"}, false, false},
	}
	for _, c := range cases {
		correct, ok := Judge(c.qtype, c.options, c.answer, c.selected, c.text)
		if correct != c.correct || ok != c.ok {
			t.Errorf("%s：got %v %v", c.name, correct, ok)
		}
	}
}
