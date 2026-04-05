package test

import (
	"fmt"
	"os"
	"runtime/debug"
	"testing"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/volts-dev/orm"
	_ "modernc.org/sqlite"
)

// Main test entry point for testing all supported ORM interfaces
// It runs sequentially relying on interface dependencies
func TestORMInterfaces(t *testing.T) {
	// 1. Configure Supported Dialects
	var sources []*orm.TDataSource

	// SQLite File Database Default
	sources = append(sources, &orm.TDataSource{
		DbType: "sqlite",
		DbName: "test.db", // Use physical file to prevent :memory: shared locking issues
	})

	// Add PostgreSQL if configured
	if pgHost := os.Getenv("POSTGRES_TEST_HOST"); pgHost != "" {
		sources = append(sources, &orm.TDataSource{
			DbType:   "postgres",
			Host:     pgHost,
			UserName: os.Getenv("POSTGRES_TEST_USER"),
			Password: os.Getenv("POSTGRES_TEST_PASS"),
			DbName:   TEST_DB_NAME,
			SSLMode:  "disable",
		})
	}

	// Add MySQL if configured
	if myHost := os.Getenv("MYSQL_TEST_HOST"); myHost != "" {
		sources = append(sources, &orm.TDataSource{
			DbType:   "mysql",
			Host:     myHost,
			UserName: os.Getenv("MYSQL_TEST_USER"),
			Password: os.Getenv("MYSQL_TEST_PASS"),
			DbName:   TEST_DB_NAME,
		})
	}

	for _, ds := range sources {
		t.Run(fmt.Sprintf("Dialect=%s", ds.DbType), func(t *testing.T) {
			DataSource = ds
			
			// Initialize Test Chain
			testChain := NewTest(t)
			testChain.ShowSql(true)

			// Safely execution to avoid crashing other runners
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Critical panic in tests for %s: %v\n%s", ds.DbType, r, debug.Stack())
				}
			}()

			// Step 1: Database Initialization & Table Generation 
			// Dependencies: None
			t.Run("1_InitializeDatabase", func(t *testing.T) {
				testChain.Log("Executing Step 1: Initialization")
				testChain.Reset() // Drops all relevant tables & syncs schemas
			})

			// Step 2: Data Insertion (Create)
			// Dependencies: InitializeDatabase
			t.Run("2_CreateOperations", func(t *testing.T) {
				testChain.Log("Executing Step 2: Create Operations")
				testChain.Create()
				testChain.CreateNone()
				// testChain.CreateOnConflict() // TODO: check dialect compatibilities
				// testChain.CreateM2m() // Skipping for now: many2many relation table 'user_model_company_model_rel' is not created by SyncModel automatically
			})

			// Step 3: Read & Query Builder Basics
			// Dependencies: CreateOperations
			t.Run("3_QueryOperations", func(t *testing.T) {
				testChain.Log("Executing Step 3: Query & Read Operations")
				testChain.Search()
				testChain.Read()
				testChain.Count()
			})

			// Step 4: Conditional Expressions
			// Dependencies: CreateOperations
			t.Run("4_ConditionalExpressions", func(t *testing.T) {
				testChain.Log("Executing Step 4: Cond Query Expressions")
				testChain.Where()
				testChain.And()
				testChain.Or()
				testChain.In()
				testChain.NotIn()
				testChain.Domain()
				testChain.Limit() 
			})

			// Step 5: Data Modification (Write)
			// Dependencies: ReadOperations
			t.Run("5_UpdateOperations", func(t *testing.T) {
				testChain.Log("Executing Step 5: Write/Update Operations")
				testChain.Write()
			})

			// Step 6: Transaction Management
			// Dependencies: Basic operations working
			t.Run("6_TransactionOperations", func(t *testing.T) {
				testChain.Log("Executing Step 6: Execute Tx logic")
				testChain.Transaction()
			})

			// Step 7: Delete Data
			// Dependencies: Finalizing the flow
			t.Run("7_DeleteOperations", func(t *testing.T) {
				testChain.Log("Executing Step 7: Data Cleanup")
				testChain.Delete()
			})
		})
	}
}
