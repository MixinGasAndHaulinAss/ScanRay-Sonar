package checks

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
)

func runSQL(ctx context.Context, params map[string]any, cred *ResolvedCred) Result {
	if cred == nil {
		return Result{OK: false, Error: "credential required", Samples: []Sample{sampleNum("up", 0)}}
	}
	host := paramString(params, "host", "")
	if host == "" {
		return Result{OK: false, Error: "host required", Samples: []Sample{sampleNum("up", 0)}}
	}
	driver := strings.ToLower(paramString(params, "driver", cred.Driver))
	if driver == "" {
		driver = "postgres"
	}
	port := paramInt(params, "port", defaultSQLPort(driver))
	query := strings.TrimSpace(paramString(params, "query", "SELECT 1"))
	if err := validateReadOnlySQL(query); err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{sampleNum("up", 0)}}
	}
	expectMin := paramInt(params, "expectMinRows", 1)
	timeout := time.Duration(paramFloat(params, "timeoutSec", 15)) * time.Second

	dsn, err := sqlDSN(driver, host, port, cred)
	if err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{sampleNum("up", 0)}}
	}
	dbDriver := sqlDriverName(driver)

	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	db, err := sql.Open(dbDriver, dsn)
	if err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{sampleNum("up", 0)}}
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(timeout)

	rows, err := db.QueryContext(rctx, query)
	ms := float64(time.Since(start).Milliseconds())
	if err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{
			sampleNum("up", 0), sampleNum("response_time_ms", ms), sampleNum("row_count", 0),
		}}
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{
			sampleNum("up", 0), sampleNum("response_time_ms", ms), sampleNum("row_count", 0),
		}}
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	count := 0
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return Result{OK: false, Error: err.Error(), Samples: []Sample{
				sampleNum("up", 0), sampleNum("response_time_ms", ms), sampleNum("row_count", float64(count)),
			}}
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return Result{OK: false, Error: err.Error(), Samples: []Sample{
			sampleNum("up", 0), sampleNum("response_time_ms", ms), sampleNum("row_count", float64(count)),
		}}
	}
	up := 0.0
	if count >= expectMin {
		up = 1
	}
	return Result{
		OK: up == 1,
		Samples: []Sample{
			sampleNum("up", up),
			sampleNum("response_time_ms", ms),
			sampleNum("row_count", float64(count)),
		},
		Error: func() string {
			if up == 1 {
				return ""
			}
			return fmt.Sprintf("expected at least %d rows, got %d", expectMin, count)
		}(),
	}
}

func defaultSQLPort(driver string) int {
	switch driver {
	case "sqlserver", "mssql":
		return 1433
	case "mysql":
		return 3306
	default:
		return 5432
	}
}

func sqlDriverName(driver string) string {
	switch driver {
	case "sqlserver", "mssql":
		return "sqlserver"
	case "mysql":
		return "mysql"
	default:
		return "pgx"
	}
}

func sqlDSN(driver, host string, port int, cred *ResolvedCred) (string, error) {
	user := cred.Username
	pass := cred.Password
	dbName := cred.Database
	if dbName == "" {
		dbName = "postgres"
	}
	switch driver {
	case "postgres", "postgresql", "pgx", "":
		ssl := cred.SSLMode
		if ssl == "" {
			ssl = "disable"
		}
		return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
			host, port, user, pass, dbName, ssl), nil
	case "mysql":
		// user:pass@tcp(host:port)/dbname
		return fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&timeout=10s",
			user, pass, net.JoinHostPort(host, strconv.Itoa(port)), dbName), nil
	case "sqlserver", "mssql":
		if dbName == "postgres" {
			dbName = "master"
		}
		return fmt.Sprintf("sqlserver://%s:%s@%s?database=%s",
			user, pass, net.JoinHostPort(host, strconv.Itoa(port)), dbName), nil
	default:
		return "", fmt.Errorf("unsupported sql driver %q", driver)
	}
}

func validateReadOnlySQL(q string) error {
	s := strings.TrimSpace(q)
	if s == "" {
		return fmt.Errorf("query required")
	}
	if strings.Contains(s, ";") {
		return fmt.Errorf("multi-statement queries are not allowed")
	}
	upper := strings.ToUpper(s)
	if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH") {
		// Reject obvious writes embedded after WITH/SELECT keywords in common patterns.
		for _, bad := range []string{" INSERT ", " UPDATE ", " DELETE ", " DROP ", " ALTER ", " CREATE ", " TRUNCATE ", " GRANT ", " REVOKE ", " COPY ", " CALL ", " EXEC ", " EXECUTE "} {
			if strings.Contains(" "+upper+" ", bad) {
				return fmt.Errorf("only read-only SELECT/WITH queries are allowed")
			}
		}
		return nil
	}
	return fmt.Errorf("only SELECT or WITH…SELECT queries are allowed")
}
