package material

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/go-sql-driver/mysql"

	"peetraining-server/internal/dbq"
)

func nullCategory(c string) dbq.NullMaterialsCategory {
	v := dbq.MaterialsCategory(c)
	return dbq.NullMaterialsCategory{MaterialsCategory: v, Valid: v.Valid()}
}

var contentTypes = map[string]string{
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pdf":  "application/pdf",
}

func contentType(f File) string {
	if ct, ok := contentTypes[f.Format]; ok {
		return ct
	}
	if strings.HasPrefix(f.ContentType, "image/") {
		return f.ContentType
	}
	return "image/jpeg"
}

func extension(f File) string {
	if f.Format != "image" {
		return f.Format
	}
	switch f.ContentType {
	case "image/png":
		return "png"
	case "image/heic":
		return "heic"
	case "image/webp":
		return "webp"
	default:
		return "jpg"
	}
}

func sha(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}
