package migrations

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/newsand/janus/internal/logger"
	"gorm.io/gorm"
)

//go:embed *.sql
var sqlFiles embed.FS

func Up(gdb *gorm.DB) error {
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}

	if _, err := sqlDB.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}

	if err := baselineIfNeeded(gdb); err != nil {
		return err
	}

	entries, err := sqlFiles.ReadDir(".")
	if err != nil {
		return err
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var n int64
		if err := gdb.Raw(
			`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, name,
		).Scan(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}

		body, err := sqlFiles.ReadFile(name)
		if err != nil {
			return err
		}

		logger.Info("Applying migration %s", name)
		tx := gdb.Begin()
		if tx.Error != nil {
			return tx.Error
		}
		if err := tx.Exec(string(body)).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := tx.Exec(
			`INSERT INTO schema_migrations (version) VALUES (?)`, name,
		).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("%s record: %w", name, err)
		}
		if err := tx.Commit().Error; err != nil {
			return err
		}
		logger.Info("Applied migration %s", name)
	}

	return nil
}

func baselineIfNeeded(gdb *gorm.DB) error {
	var migCount int64
	if err := gdb.Raw(`SELECT COUNT(1) FROM schema_migrations`).Scan(&migCount).Error; err != nil {
		return err
	}
	if migCount > 0 {
		return nil
	}

	var usersExist bool
	if err := gdb.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'users'
	)`).Scan(&usersExist).Error; err != nil {
		return err
	}
	if !usersExist {
		return nil
	}

	logger.Info("Baselining schema_migrations for existing schema (001_initial.sql)")
	return gdb.Exec(
		`INSERT INTO schema_migrations (version) VALUES (?)`, "001_initial.sql",
	).Error
}
