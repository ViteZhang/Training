// eval 跑 AI 能力评测：读 evals/private/ 的样本与人工标注，输出指标与是否达标（门槛见 PRD v3 12.3）。
//
// 各项评测在对应任务卡里实现：import、kp（T10）、grading（T18）、essay（T23）、ocr（T19）。
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
)

var capabilities = []string{"import", "kp", "grading", "essay", "ocr"}

func main() {
	capability := flag.String("cap", "", "评测能力：import、kp、grading、essay、ocr")
	flag.Parse()
	if !slices.Contains(capabilities, *capability) {
		fmt.Fprintf(os.Stderr, "eval: 请用 -cap 指定能力，可选 %v\n", capabilities)
		os.Exit(2)
	}
	fmt.Fprintf(os.Stderr, "eval: %s 评测尚未实现（见 docs/tasks 对应卡片）\n", *capability)
	os.Exit(1)
}
