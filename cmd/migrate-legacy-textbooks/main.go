// 显式运维命令，不自动加载 .env.local，不执行 schema 迁移或触发索引/发布。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"peetraining-server/internal/store"
	"peetraining-server/internal/textbook"
)

func main() {
	apply := flag.Bool("apply", false, "create unpublished, unauthorized drafts (default dry-run)")
	actor := flag.Uint64("actor", 0, "administrator id recorded for apply")
	flag.Parse()
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" || (*apply && *actor == 0) {
		fmt.Fprintln(os.Stderr, "MYSQL_DSN is required; --apply also requires --actor=<administrator id>")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	db, err := store.OpenMySQL(ctx, dsn)
	if err != nil {
		fail()
	}
	defer func() { _ = db.Close() }()
	rows, err := textbook.New(db).MigrateLegacyReferences(ctx, *actor, *apply)
	if err != nil {
		fail()
	}
	if json.NewEncoder(os.Stdout).Encode(map[string]any{"apply": *apply, "items": rows}) != nil {
		fail()
	}
	for _, r := range rows {
		if r.Status == "source_changed" {
			os.Exit(1)
		}
	}
}
func fail() {
	fmt.Fprintln(os.Stderr, "Legacy migration failed; no credentials or original text are logged. Check database connectivity/schema and retry dry-run.")
	os.Exit(1)
}
