// Package log provides a Zerolog-based logger that writes JSON logs to an SQLite database.
package log

import (
	"database/sql"
	"errors" // Import errors package
	"fmt"
	stdlog "log" // Use alias to avoid conflict with package name
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	//_ "github.com/mattn/go-sqlite3" // SQLite driver
	"n2n-go/pkg/appdir"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

// --- Global state for the package logger ---

var (
	writeSinceStart        atomic.Int64
	pkgLogger              = zerolog.Nop() // Default to no-op logger
	dbWriterInstance       *sqliteWriter
	dbHandle               *sql.DB      // The single handle used for writing and reading
	mu                     sync.RWMutex // Protects access to dbHandle and pkgLogger during Init/Close
	defaultDbPath          = "./logs.db"
	zerologTimeFieldFormat = time.RFC3339Nano
	// Error returned when trying to use retrieval functions before Init
	ErrNotInitialized = errors.New("log: logger not initialized, call log.Init() first")
)

// --- Custom io.Writer for SQLite (sqliteWriter struct and methods remain the same) ---
type sqliteWriter struct {
	db   *sql.DB
	stmt *sql.Stmt
	mu   sync.Mutex // Protect concurrent writes to the statement
}

func newSQLiteWriter(dbPath string) (*sqliteWriter, *sql.DB, error) {
	dsn := fmt.Sprintf("%s?_pragma=journal_mode=wal&_pragma=busy_timeout=5000", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open sqlite db %s: %w", dbPath, err)
	}
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to ping sqlite db %s: %w", dbPath, err)
	}

	createTableSQL := `
    CREATE TABLE IF NOT EXISTS logs (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        inserted_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL,
        log_data TEXT NOT NULL
    );`
	_, err = db.Exec(createTableSQL)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to create logs table: %w", err)
	}

	createIndexSQLTime := `CREATE INDEX IF NOT EXISTS idx_logs_json_time ON logs (json_extract(log_data, '$.time'));`
	_, err = db.Exec(createIndexSQLTime)
	if err != nil {
		stdlog.Printf("Warning: Failed to create JSON time index: %v. Performance for time-based queries might be reduced.\n", err)
	}

	createIndexSQLLevel := `CREATE INDEX IF NOT EXISTS idx_logs_json_level ON logs (json_extract(log_data, '$.level'));`
	_, err = db.Exec(createIndexSQLLevel)
	if err != nil {
		stdlog.Printf("Warning: Failed to create JSON level index: %v\n", err)
	}

	insertSQL := `INSERT INTO logs (log_data) VALUES (?)`
	stmt, err := db.Prepare(insertSQL)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to prepare insert statement: %w", err)
	}

	writer := &sqliteWriter{
		db:   db,
		stmt: stmt,
	}
	return writer, db, nil
}

func (w *sqliteWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.stmt.Exec(string(p))
	if err != nil {
		stdlog.Printf("ERROR writing log to SQLite: %v\n", err)
		return 0, err
	}
	writeSinceStart.Add(1)
	return len(p), nil
}

func (w *sqliteWriter) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	var firstErr error
	if w.stmt != nil {
		err := w.stmt.Close()
		if err != nil {
			firstErr = fmt.Errorf("error closing statement: %w", err)
		}
		w.stmt = nil
	}
	if w.db != nil {
		err := w.db.Close()
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("error closing db: %w", err)
		} else if err != nil {
			firstErr = fmt.Errorf("%v; error closing db: %w", firstErr, err)
		}
		w.db = nil
	}
	return firstErr
}

// --- Package Initialization and Configuration ---

func SetStd() {
	pkgLogger = zerolog.New(zlog.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339})).With().Timestamp().Logger()
}

func Init(dbFile string) error {

	if dbFile == "" {
		return fmt.Errorf("logger need an explicity dbFile")
	}

	dbPath := path.Join(appdir.AppDir(), dbFile)

	mu.Lock()
	defer mu.Unlock()

	if dbWriterInstance != nil {
		return fmt.Errorf("logger already initialized")
	}

	path := dbPath
	if path == "" {
		path = defaultDbPath
	}

	writer, db, err := newSQLiteWriter(path)
	if err != nil {
		return fmt.Errorf("failed to create SQLite writer: %w", err)
	}

	dbWriterInstance = writer
	dbHandle = db // Store the handle globally

	zerolog.TimeFieldFormat = zerologTimeFieldFormat
	pkgLogger = zerolog.New(dbWriterInstance).With().
		Timestamp().
		Logger()

	stdlog.Printf("Zerolog SQLite logger initialized writing to %s\n", path)
	return nil
}

func Close() error {
	mu.Lock()
	defer mu.Unlock()

	if dbWriterInstance == nil {
		stdlog.Println("Logger Close() called but not initialized or already closed.")
		return nil
	}

	// Ensure subsequent reads know the DB is closing/closed
	//handle := dbHandle
	dbHandle = nil // Set global handle to nil first under lock
	dbWriter := dbWriterInstance
	dbWriterInstance = nil
	pkgLogger = zerolog.Nop()

	// Log shutdown message *using the writer before closing it*
	writerLogger := zerolog.New(dbWriter).With().Timestamp().Logger()
	writerLogger.Log().Msg("Closing SQLite logger")

	err := dbWriter.close() // This now closes the handle that was stored in dbHandle

	if err != nil {
		// Log closing error to standard logger as pkgLogger is now Nop
		stdlog.Printf("Error closing SQLite logger: %v\n", err)
		return fmt.Errorf("error closing SQLite logger: %w", err)
	}
	stdlog.Println("Zerolog SQLite logger closed.")
	return nil
}

// --- Logging Functions (Debug, Info, Warn, Error, Fatal, Panic, Log - remain the same) ---
func Debug() *zerolog.Event { return pkgLogger.Debug() }
func Info() *zerolog.Event  { return pkgLogger.Info() }
func Warn() *zerolog.Event  { return pkgLogger.Warn() }
func Error() *zerolog.Event { return pkgLogger.Error() }
func Fatal() *zerolog.Event { return pkgLogger.Fatal() }
func Panic() *zerolog.Event { return pkgLogger.Panic() }
func Log() *zerolog.Event   { return pkgLogger.Log() }

// Print sends a log event using debug level and no extra field.
// Arguments are handled in the manner of fmt.Print.
func Print(v ...interface{}) {
	pkgLogger.Info().CallerSkipFrame(1).Msg(fmt.Sprint(v...))
}

// Printf sends a log event using debug level and no extra field.
// Arguments are handled in the manner of fmt.Printf.
func Printf(format string, v ...interface{}) {
	pkgLogger.Info().CallerSkipFrame(1).Msgf(format, v...)
}

func Fatalf(format string, v ...any) {
	pkgLogger.Fatal().Msgf(format, v...)
}

// --- Log Retrieval Functions (Simplified API) ---

type LogEntry struct {
	ID         int64
	InsertedAt time.Time
	LogData    string // The raw JSON string
}

const (
	DefaultLimit = 100
)

// getHandle provides safe concurrent access to the global dbHandle.
// Returns the handle and nil error if initialized, or nil and ErrNotInitialized otherwise.
func getHandle() (*sql.DB, error) {
	mu.RLock()
	defer mu.RUnlock()
	if dbHandle == nil {
		return nil, ErrNotInitialized
	}
	return dbHandle, nil
}

// parseDBTimestamp tries common SQLite timestamp formats.
func parseDBTimestamp(ts string) time.Time {
	// Add more formats here if needed, in order of likelihood
	formats := []string{
		"2006-01-02 15:04:05",     // SQLite default without timezone
		time.RFC3339,              // ISO 8601 with timezone
		time.RFC3339Nano,          // ISO 8601 with timezone and nanoseconds
		"2006-01-02 15:04:05.999", // Common fractional seconds format
		time.DateTime,             // Go's 2006-01-02 15:04:05 format
	}
	for _, format := range formats {
		t, err := time.Parse(format, ts)
		if err == nil {
			return t
		}
	}
	// Log warning if parsing fails completely
	stdlog.Printf("Warning: Could not parse inserted_at timestamp '%s' with known formats", ts)
	return time.Time{} // Return zero time if unparseable
}

// GetLogsSince start uses the writeSinceStart counter to retrieve the last N logs since current edge start
func GetLogsSinceStart() ([]LogEntry, error) {
	n := writeSinceStart.Load()
	return GetLastNLogs(int(n))
}

// GetLastNLogs retrieves the most recent 'n' log entries using the internal DB handle.
// Logs are returned in chronological order (oldest of the 'n' first).
// Returns ErrNotInitialized if log.Init() has not been called.
func GetLastNLogs(n int) ([]LogEntry, error) {
	handle, err := getHandle()
	if err != nil {
		return nil, err // Return ErrNotInitialized
	}
	if n <= 0 {
		return []LogEntry{}, nil
	}

	query := `SELECT id, inserted_at, log_data FROM logs ORDER BY id DESC LIMIT ?`
	rows, err := handle.Query(query, n)
	if err != nil {
		return nil, fmt.Errorf("failed to query last %d logs: %w", n, err)
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var entry LogEntry
		var insertedAtStr string
		if err := rows.Scan(&entry.ID, &insertedAtStr, &entry.LogData); err != nil {
			return nil, fmt.Errorf("failed to scan log entry: %w", err)
		}
		entry.InsertedAt = parseDBTimestamp(insertedAtStr)
		logs = append(logs, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating log rows: %w", err)
	}

	// Reverse the slice
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}

	return logs, nil
}

// GetLogsBetween retrieves log entries using the internal DB handle where the log event time
// (from JSON 'time' field) falls within the specified start and end times (inclusive).
// Logs are returned in chronological order (by event time).
// A limit <= 0 means default limit (DefaultLimit).
// Returns ErrNotInitialized if log.Init() has not been called.
func GetLogsBetween(start, end time.Time, limit int) ([]LogEntry, error) {
	handle, err := getHandle()
	if err != nil {
		return nil, err // Return ErrNotInitialized
	}

	effectiveLimit := limit
	if effectiveLimit <= 0 {
		effectiveLimit = DefaultLimit
	}

	startTimeStr := start.Format(zerologTimeFieldFormat)
	endTimeStr := end.Format(zerologTimeFieldFormat)

	query := `
        SELECT id, inserted_at, log_data
        FROM logs
        WHERE json_extract(log_data, '$.time') >= ? AND json_extract(log_data, '$.time') <= ?
        ORDER BY json_extract(log_data, '$.time') ASC, id ASC
        LIMIT ?`

	rows, err := handle.Query(query, startTimeStr, endTimeStr, effectiveLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to query logs between %s and %s: %w", startTimeStr, endTimeStr, err)
	}
	defer rows.Close()

	var logs []LogEntry
	for rows.Next() {
		var entry LogEntry
		var insertedAtStr string
		if err := rows.Scan(&entry.ID, &insertedAtStr, &entry.LogData); err != nil {
			return nil, fmt.Errorf("failed to scan log entry: %w", err)
		}
		entry.InsertedAt = parseDBTimestamp(insertedAtStr)
		logs = append(logs, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating log rows: %w", err)
	}

	return logs, nil
}

// GetLogsSince retrieves log entries using the internal DB handle where the log event time
// is after or equal to the specified start time, up to the current time.
// It's a convenience wrapper around GetLogsBetween.
// Logs are returned in chronological order (by event time).
// A limit <= 0 means default limit (DefaultLimit).
// Returns ErrNotInitialized if log.Init() has not been called.
func GetLogsSince(start time.Time, limit int) ([]LogEntry, error) {
	// No need to call getHandle() here, as GetLogsBetween will do it.
	return GetLogsBetween(start, time.Now(), limit)
}
