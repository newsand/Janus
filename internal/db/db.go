package db

import (
	"time"

	"github.com/newsand/janus/internal/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var gormDB *gorm.DB

func Init(databaseURL string) error {
	config := &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	}

	var err error
	gormDB, err = gorm.Open(postgres.Open(databaseURL), config)
	if err != nil {
		return err
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		return err
	}

	// Idle matches open so a burst does not pay a new handshake per request.
	// Stay under a typical Postgres max_connections, leaving room for admin sessions.
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(time.Hour)
	sqlDB.SetConnMaxIdleTime(30 * time.Minute)

	if err := sqlDB.Ping(); err != nil {
		return err
	}

	logger.Info("Database connection established")
	return nil
}

func DB() *gorm.DB {
	return gormDB
}

// EnablePreparedStatements caches query plans for the life of the process.
// Call it after migrations: a prepared statement cannot hold the multi-statement
// migration scripts, and a plan cached before DDL would be stale.
func EnablePreparedStatements() {
	if gormDB == nil || gormDB.PrepareStmt {
		return
	}
	prepared := gorm.NewPreparedStmtDB(gormDB.ConnPool, 0, 0)
	gormDB.PrepareStmt = true
	gormDB.ConnPool = prepared
	if gormDB.Statement != nil {
		gormDB.Statement.ConnPool = prepared
	}
}

func SetTestDB(testDB *gorm.DB) {
	gormDB = testDB
}

func Close() {
	if gormDB != nil {
		sqlDB, _ := gormDB.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
		logger.Info("Database connection closed")
	}
}

func HealthCheck() error {
	sqlDB, err := gormDB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}
