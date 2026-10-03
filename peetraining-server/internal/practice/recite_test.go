package practice

import (
	"testing"

	"peetraining-server/internal/rules"
)

func TestSegmentsAndCoverage(t *testing.T) {
	orig := "建安风骨，指以三曹和建安七子为代表的作家，形成慷慨悲凉、刚健有力的风格。"
	kws := []string{"三曹", "建安七子", "慷慨悲凉", "刚健有力", "建安"}
	segs := segmentsOf(orig, kws)
	joined, blanks := "", 0
	for _, s := range segs {
		joined += s.Text
		if s.Blank {
			blanks++
		}
	}
	if joined != orig || blanks != 5 {
		t.Errorf("切段：%+v", segs)
	}
	hits := coverage("以三曹 和《建安七子》为代表，慷慨悲凉", kws[:4])
	if !hits[0].Hit || !hits[1].Hit || !hits[2].Hit || hits[3].Hit {
		t.Errorf("覆盖：%+v", hits)
	}
	if resultOf(hits) != rules.ReciteVague || resultOf(coverage(orig, kws)) != rules.ReciteRemembered || resultOf(coverage("无关", kws)) != rules.ReciteForgot {
		t.Error("按覆盖判定：全部为记住了，过半为模糊，其余没记住")
	}
}
