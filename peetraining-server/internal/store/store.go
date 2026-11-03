package store

import (
	"errors"

	"github.com/go-sql-driver/mysql"
)

// IsDuplicate 判断唯一键冲突（MySQL 1062）。
func IsDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}
