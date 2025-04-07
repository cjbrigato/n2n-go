package main

import (
	"errors"
	"fmt"
	"n2n-go/pkg/log"
	"os"
	"time"

	"github.com/urfave/cli/v2" // Import urfave/cli
)

/*
func logs(s string) {
	mgmt, err := edge.NewEdgeManagementClient()
	if err == nil {
		res, err := mgmt.SendCommand(fmt.Sprintf("logs %s", s))
		if err != nil {
			stdlog.Fatalf("%v", err)
		}
		fmt.Println(res)
		os.Exit(0)
	}
	edge.EnsureEdgeLogger()
	entries, err := log.GetLastNLogs(20)
	if err != nil {
		stdlog.Fatalf("%v", err)
	}
	for _, e := range entries {
		fmt.Println(e)
	}
	os.Exit(0)
}*/

// --- Time Parsing Helper ---

// timeFormats includes common layouts to try when parsing absolute time strings.
// Order matters; more specific formats should generally come earlier.
var timeFormats = []string{
	time.RFC3339Nano,      // "2006-01-02T15:04:05.999999999Z07:00"
	time.RFC3339,          // "2006-01-02T15:04:05Z07:00"
	"2006-01-02T15:04:05", // ISO 8601 without timezone
	"2006-01-02 15:04:05", // Common space-separated format
	"2006-01-02",          // Date only
}

// parseTimeSpec attempts to parse a string as either a relative duration
// from now (e.g., "1h", "30m") or an absolute timestamp using various layouts.
func parseTimeSpec(spec string) (time.Time, error) {
	// 1. Try parsing as relative duration (negative offset from now)
	duration, err := time.ParseDuration(spec)
	if err == nil {
		// It's a valid duration string, calculate time relative to now
		// Durations are positive, so we add a negative duration.
		return time.Now().Add(-duration), nil
	}

	// 2. Try parsing as absolute timestamp using known formats
	for _, layout := range timeFormats {
		ts, err := time.Parse(layout, spec)
		if err == nil {
			return ts, nil // Successfully parsed
		}
	}

	// 3. If all parsing fails
	return time.Time{}, fmt.Errorf("invalid time specification: '%s'. Use relative duration (e.g., '1h', '30m') or absolute format (e.g., '2023-10-27T15:04:05Z')", spec)
}

// --- Custom Help Template ---

// Define the custom help template string matching the design
const logsCommandHelpTemplate = `NAME:
   {{.HelpName}} - {{.Usage}}

USAGE:
   {{.HelpName}} {{if .UsageText}}{{.UsageText}}{{else}}[command options] argument...{{end}}
{{if .Description}}
DESCRIPTION:
   {{.Description | Indent 4}}
{{end}}
MODES (choose one; defaults to --last if no mode specified):
     --last                 Retrieve the most recent N log entries.
                            (This is the default mode if no other mode flag is provided).
     --since                Retrieve logs since a specific start time up to now.
     --between              Retrieve logs between a specific start and end time.

OPTIONS:
{{range .VisibleFlags}}   {{.}}
{{end}}
TIME SPECIFICATION (<time_spec>):
     You can specify time in two ways:
     1. Relative Duration: A duration string relative to the current time.
        Examples: "5m" (5 minutes ago), "1h30m" (1 hour 30 minutes ago),
                  "2d" (2 days ago), "1w" (1 week ago).
        Units: s (seconds), m (minutes), h (hours), d (days), w (weeks).
     2. Absolute Timestamp: An RFC3339 or similar ISO 8601 format timestamp.
        The tool should attempt to parse common formats. Timezone assumed
        local if not specified, or use 'Z' for UTC.
        Examples: "2023-10-27T15:04:05Z", "2023-10-27 10:00:00", "2023-10-27".

EXAMPLES:
     # Get the last 50 log entries (defaulting to --last mode)
     myapp logs -f /var/log/app.db -n 50

     # Get the last 100 log entries (using default count and mode)
     myapp logs -f /var/log/app.db

     # Get logs since 1 hour ago, max 500 entries, in pretty format
     myapp logs -f /var/log/app.db --since -s 1h -l 500 --pretty

     # Get logs since a specific UTC timestamp
     myapp logs -f /var/log/app.db --since -s "2023-10-26T10:00:00Z"

     # Get logs between 2 days ago and 1 day ago
     myapp logs -f /var/log/app.db --between -s 2d -e 1d

     # Get logs between two specific dates (local timezone assumed)
     myapp logs -f /var/log/app.db --between -s "2023-10-20" -e "2023-10-25" --limit 2000

`

// --- CLI Definition ---

var (
	// Define the 'logs' subcommand
	logsCommand = &cli.Command{
		Name:               "logs",
		Usage:              "Retrieve JSON log entries from the application's log database",
		UsageText:          "myapp logs [command options] [--last|--since|--between] [mode options]",
		Description:        `Retrieves logs stored in an SQLite database specified with -f/--dbfile.`,
		CustomHelpTemplate: logsCommandHelpTemplate,
		Flags: []cli.Flag{
			// --- Common Options ---
			&cli.StringFlag{
				Name:     "dbfile",
				Aliases:  []string{"f"},
				Usage:    "Path to the SQLite log database file `PATH` (required)",
				Required: true,
			},
			&cli.BoolFlag{
				Name:    "pretty",                                                                     // New Flag
				Aliases: []string{"p"},                                                                // New Flag
				Usage:   "Output logs in a human-readable, pretty-printed format instead of raw JSON", // New Flag
				Value:   false,                                                                        // Defaults to false (raw JSON output)
			},

			// --- Mode Flags ---
			&cli.BoolFlag{
				Name:  "last",
				Usage: "Mode: Retrieve the most recent N log entries (default)",
			},
			&cli.BoolFlag{
				Name:  "since",
				Usage: "Mode: Retrieve logs since a specific start time",
			},
			&cli.BoolFlag{
				Name:  "between",
				Usage: "Mode: Retrieve logs between a specific start and end time",
			},

			// --- Options for --last ---
			&cli.IntFlag{
				Name:    "count",
				Aliases: []string{"n"},
				Usage:   "Number of entries for --last mode `NUMBER`",
				Value:   100,
			},

			// --- Options for --since / --between ---
			&cli.StringFlag{
				Name:    "start",
				Aliases: []string{"s"},
				Usage:   "Start time for --since/--between `TIME_SPEC` (e.g., '1h', '2023-10-27T10:00:00Z')",
			},
			&cli.StringFlag{
				Name:    "end",
				Aliases: []string{"e"},
				Usage:   "End time for --between `TIME_SPEC` (e.g., '30m', '2023-10-27T11:00:00')",
			},
			&cli.IntFlag{
				Name:    "limit",
				Aliases: []string{"l"},
				Usage:   "Max entries for --since/--between `NUMBER`",
				Value:   1000,
			},
		},
		Action: logsCmd,
	}
)

func logsCmd(c *cli.Context) error {
	// --- Flag & Mode Validation ---
	dbFile := c.String("dbfile")
	isPretty := c.Bool("pretty") // Get the value of the new flag

	isLast := c.Bool("last")
	isSince := c.Bool("since")
	isBetween := c.Bool("between")

	modeCount := 0
	if isLast {
		modeCount++
	}
	if isSince {
		modeCount++
	}
	if isBetween {
		modeCount++
	}

	if modeCount == 0 {
		isLast = true
	} else if modeCount > 1 {
		return cli.Exit("Error: Only one mode flag (--last, --since, --between) can be specified at a time.", 1)
	}

	// --- Initialize Logger ---
	// Note: We init the log package even if we are just reading,
	// as it sets up the db handle internally. Consider optimizing
	// this if initialization becomes heavy and you only need reads.
	err := log.Init(dbFile)
	if err != nil {
		if os.IsNotExist(err) {
			return cli.Exit(fmt.Sprintf("Error: Database file not found at '%s'", dbFile), 1)
		}
		return cli.Exit(fmt.Sprintf("Error initializing logger (required for DB access): %v", err), 1)
	}
	defer log.Close() // Ensure DB connection is closed eventually

	// --- Execute Action Based on Mode ---
	var results []log.LogEntry
	var retrievalErr error

	if isLast {
		if c.IsSet("start") || c.IsSet("end") {
			fmt.Fprintln(os.Stderr, "Warning: --start (-s) and --end (-e) flags are ignored in --last mode.")
		}
		count := c.Int("count")
		if count <= 0 {
			return cli.Exit("Error: --count (-n) must be a positive number.", 1)
		}
		results, retrievalErr = log.GetLastNLogs(count)

	} else if isSince {
		if !c.IsSet("start") {
			return cli.Exit("Error: --start (-s) flag is required for --since mode.", 1)
		}
		if c.IsSet("end") {
			fmt.Fprintln(os.Stderr, "Warning: --end (-e) flag is ignored in --since mode.")
		}
		startTimeSpec := c.String("start")
		startTime, err := parseTimeSpec(startTimeSpec)
		if err != nil {
			return cli.Exit(fmt.Sprintf("Error parsing start time: %v", err), 1)
		}
		limit := c.Int("limit")
		results, retrievalErr = log.GetLogsSince(startTime, limit)

	} else if isBetween {
		if !c.IsSet("start") {
			return cli.Exit("Error: --start (-s) flag is required for --between mode.", 1)
		}
		if !c.IsSet("end") {
			return cli.Exit("Error: --end (-e) flag is required for --between mode.", 1)
		}
		startTimeSpec := c.String("start")
		endTimeSpec := c.String("end")
		startTime, err := parseTimeSpec(startTimeSpec)
		if err != nil {
			return cli.Exit(fmt.Sprintf("Error parsing start time: %v", err), 1)
		}
		endTime, err := parseTimeSpec(endTimeSpec)
		if err != nil {
			return cli.Exit(fmt.Sprintf("Error parsing end time: %v", err), 1)
		}
		if startTime.After(endTime) {
			fmt.Fprintf(os.Stderr, "Warning: Start time (%s) is after end time (%s).\n", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
		}
		limit := c.Int("limit")
		results, retrievalErr = log.GetLogsBetween(startTime, endTime, limit)
	}

	// --- Handle Retrieval Errors ---
	if retrievalErr != nil {
		if errors.Is(retrievalErr, log.ErrNotInitialized) {
			// This error path assumes Init might fail *after* the initial check somehow,
			// or if Close was called prematurely in complex scenarios.
			return cli.Exit("Internal Error: Logger DB handle became unavailable.", 2)
		}
		// Handle potential SQL errors etc.
		return cli.Exit(fmt.Sprintf("Error retrieving logs: %v", retrievalErr), 1)
	}

	// --- Output Results ---
	if len(results) == 0 {
		fmt.Fprintln(os.Stderr, "No log entries found matching the criteria.")
		return nil // Successful execution, just no results
	}

	// --- Output Logic ---
	if isPretty {
		// <<< PLACEHOLDER FOR YOUR PRETTY PRINTING LOGIC >>>
		// You would iterate through 'results' and format each 'entry.LogData'
		// (which is a JSON string) nicely here.
		// Example: Decode JSON, extract fields, colorize, format timestamp, etc.
		fmt.Fprintln(os.Stderr, "Pretty-printing requested (implementation pending)...")
		for i, entry := range results {
			// Extremely basic example - just print index and raw JSON for now
			fmt.Printf("Entry %d (ID: %d, Inserted: %s):\n%s\n---\n",
				i+1,
				entry.ID,
				entry.InsertedAt.Format(time.RFC3339),
				entry.LogData, // Replace this with your actual pretty printing
			)
		}
		// <<< END PLACEHOLDER >>>
	} else {
		// Default: Output raw JSON lines
		for _, entry := range results {
			fmt.Println(entry.LogData)
		}
	}

	return nil // Success
}
