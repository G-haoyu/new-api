package model

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TEST_MYSQL_DSN / TEST_POSTGRES_DSN must point to disposable databases.
// TEST_*_LOG_DSN optionally selects a separate log database.

func useDialectAnalyticsDB(t *testing.T, mainDB, logDB *gorm.DB, dbType common.DatabaseType) {
	t.Helper()
	previousDB, previousLogDB := DB, LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	DB, LOG_DB = mainDB, logDB
	common.SetDatabaseTypes(dbType, dbType)
	initCol()
	t.Cleanup(func() {
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		initCol()
	})

	require.NoError(t, mainDB.AutoMigrate(&User{}, &Channel{}, &Token{}, &QuotaData{}, &Log{}))
	if logDB != mainDB {
		require.NoError(t, logDB.AutoMigrate(&Log{}))
	}
}

func openDialectAnalyticsDB(t *testing.T, dbType common.DatabaseType, dsn string) *gorm.DB {
	t.Helper()
	var db *gorm.DB
	var err error
	if dbType == common.DatabaseTypePostgreSQL {
		db, err = gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), &gorm.Config{})
	} else {
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	}
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func runUsageAnalyticsDialectSuite(t *testing.T, dbType common.DatabaseType, mainEnv, logEnv string) {
	mainDSN := strings.TrimSpace(os.Getenv(mainEnv))
	if mainDSN == "" {
		t.Skip(mainEnv + " is not configured")
	}
	mainDB := openDialectAnalyticsDB(t, dbType, mainDSN)
	logDB := mainDB
	if logDSN := strings.TrimSpace(os.Getenv(logEnv)); logDSN != "" {
		logDB = openDialectAnalyticsDB(t, dbType, logDSN)
	}
	useDialectAnalyticsDB(t, mainDB, logDB, dbType)

	for name, run := range map[string]func(*testing.T){
		"summary closed range":       TestGetUsageAnalyticsSummaryAggregatesClosedRange,
		"summary filters":            TestGetUsageAnalyticsSummaryAppliesFilters,
		"group by model":             TestGetUsageAnalyticsGroupByModel,
		"group by user":              TestGetUsageAnalyticsGroupByUserJoinsDisplayName,
		"group by user model":        TestGetUsageAnalyticsGroupByUserModel,
		"group by model channel":     TestGetUsageAnalyticsGroupByModelChannelJoinsChannelName,
		"consume log columns":        TestRecordConsumeLogWritesExtendedQuotaDataColumns,
		"failure flush increments":   TestLogQuotaDataFinalFailureAccumulatesAcrossFlushes,
		"legacy readers skip failed": TestLegacyQuotaDataReadersSkipFailureOnlyRows,
	} {
		t.Run(name, run)
	}
}

func TestUsageAnalyticsMySQL(t *testing.T) {
	runUsageAnalyticsDialectSuite(t, common.DatabaseTypeMySQL, "TEST_MYSQL_DSN", "TEST_MYSQL_LOG_DSN")
}

func TestUsageAnalyticsPostgreSQL(t *testing.T) {
	runUsageAnalyticsDialectSuite(t, common.DatabaseTypePostgreSQL, "TEST_POSTGRES_DSN", "TEST_POSTGRES_LOG_DSN")
}
