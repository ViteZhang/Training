package rules

import "testing"

func TestTextbookImportLimitsAndNames(t *testing.T) {
	l := TextbookImportLimits{MaxConcurrentImports: 2, MaxFileMB: 50, MaxPages: 1000, MaxImportsPerRevision: 50, MaxDailyPDFPages: 2000}
	f := TextbookImportFile{Name: "教材.pdf", Format: "pdf", Size: 10, SHA256: "ab00000000000000000000000000000000000000000000000000000000000000"}
	if err := ValidateTextbookImport(f, l); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../教材.pdf", "目录/教材.pdf", "教材.docx", "教材.pdf\x00"} {
		bad := f
		bad.Name = name
		if err := ValidateTextbookImport(bad, l); err == nil {
			t.Fatal("无效名称未拒绝", name)
		}
	}
	l.MaxConcurrentImports = 0
	if err := ValidateTextbookImportLimits(l); err == nil {
		t.Fatal("无效并发配置未拒绝")
	}
}
