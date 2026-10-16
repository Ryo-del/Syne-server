package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestErrorReportsPersistAndCount(t *testing.T) {
	database, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := initSyncSchema(database); err != nil {
		t.Fatal(err)
	}
	if err := AddErrorReport(database, "student", "Имя Фамилия", "192.0.2.4", "Не удалось открыть файл"); err != nil {
		t.Fatal(err)
	}
	rows, err := ListErrorReports(database, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Имя Фамилия" || rows[0].IP != "192.0.2.4" || rows[0].Error != "Не удалось открыть файл" {
		t.Fatalf("unexpected reports: %#v", rows)
	}
	count, err := CountErrorReports(database)
	if err != nil || count != 1 {
		t.Fatalf("count = %d, err = %v", count, err)
	}
}
