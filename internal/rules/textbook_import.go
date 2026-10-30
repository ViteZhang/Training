package rules

import (
	"encoding/hex"
	"path/filepath"
	"peetraining-server/internal/apperr"
	"strings"
	"unicode/utf8"
)

type TextbookImportLimits struct {
	MaxConcurrentImports  int `json:"max_concurrent_imports"`
	MaxFileMB             int `json:"max_file_mb"`
	MaxPages              int `json:"max_pages"`
	MaxImportsPerRevision int `json:"max_imports_per_revision"`
	MaxDailyPDFPages      int `json:"max_daily_pdf_pages"`
}
type TextbookImportFile struct {
	Name   string `json:"file_name"`
	Format string `json:"format"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func ValidateTextbookImport(f TextbookImportFile, l TextbookImportLimits) error {
	if err := ValidateTextbookImportLimits(l); err != nil {
		return err
	}
	if f.Name != filepath.Base(f.Name) || strings.ContainsAny(f.Name, "/\\\x00\r\n") || !utf8.ValidString(f.Name) || !boundedText(f.Name, 1, 255) {
		return apperr.New(apperr.BadRequest, "文件名称不正确")
	}
	if f.Format != "pdf" && f.Format != "docx" && f.Format != "txt" && f.Format != "md" {
		return apperr.New(apperr.BadRequest, "仅支持 PDF、DOCX、Markdown、TXT")
	}
	if strings.ToLower(filepath.Ext(f.Name)) != "."+f.Format {
		return apperr.New(apperr.BadRequest, "文件扩展名与格式不一致")
	}
	h, err := hex.DecodeString(f.SHA256)
	if err != nil || len(h) != 32 || f.SHA256 != strings.ToLower(f.SHA256) || f.Size < 1 || f.Size > int64(l.MaxFileMB)<<20 {
		return apperr.New(apperr.BadRequest, "文件大小或校验值不正确")
	}
	return nil
}

func ValidateTextbookImportLimits(l TextbookImportLimits) error {
	if l.MaxConcurrentImports < 1 || l.MaxConcurrentImports > 4 || l.MaxFileMB < 1 || l.MaxFileMB > 200 || l.MaxPages < 1 || l.MaxPages > 5000 || l.MaxImportsPerRevision < 1 || l.MaxImportsPerRevision > 200 || l.MaxDailyPDFPages < 1 {
		return apperr.New(apperr.BadRequest, "教材导入参数无效，请联系管理员")
	}
	return nil
}
