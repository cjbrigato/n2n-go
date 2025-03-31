package management

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"n2n-go/pkg/log"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	defaultSocketDir = "/run/n2n-go"
)

func GetDefaultSocketPath(app string) string {
	return fmt.Sprintf("%s/%s", defaultSocketDir, app)
}

// CommandHandler defines the function signature for handling commands.
// It receives the command arguments and should return a response string and an error.
type CommandHandler func(args []string) (string, error)

// CommandInfo holds the handler function and its description.
type CommandInfo struct {
	Handler     CommandHandler
	Description string
}

// ManagementServer manages the Unix socket listener for daemon control.
type ManagementServer struct {
	socketPath string
	listener   net.Listener
	handlers   map[string]CommandInfo // <-- Changed type
	mu         sync.RWMutex           // Protects handlers map
	quit       chan struct{}
	wg         sync.WaitGroup
	startTime  time.Time
	password   string // <-- Add password field
}

func ensureSocketDir() {
	dir := defaultSocketDir
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.Mkdir(dir, 0755)
	}
}

// NewManagementServer creates a new management server.
func NewManagementServer(app string, password string) *ManagementServer {
	s := &ManagementServer{
		socketPath: GetDefaultSocketPath(app),
		handlers:   make(map[string]CommandInfo), // <-- Changed type
		quit:       make(chan struct{}),
		startTime:  time.Now(),
		password:   password, // <-- Store password
	}
	ensureSocketDir()
	// Register a default "status" handler
	s.RegisterHandler("status", "Show daemon status and uptime", s.handleStatusCommand)
	s.RegisterHandler("ping", "Check if the daemon's management interface is responsive", s.handlePingCommand)
	s.RegisterHandler("logs", "Get last logs from edge", s.handleLogsCommand)
	s.RegisterHandler("help", "Show help for commands. Usage: help [command]", s.handleHelpCommand)
	s.RegisterHandler("list", "Alias for 'help'", s.handleHelpCommand) // Register alias with same handler/desc
	return s
}

// RegisterHandler adds a command handler along with its description.
func (s *ManagementServer) RegisterHandler(command, description string, handler CommandHandler) { // <-- Added description param
	s.mu.Lock()
	defer s.mu.Unlock()
	lowerCommand := strings.ToLower(command) // Ensure command is stored lowercase
	if _, exists := s.handlers[lowerCommand]; exists {
		log.Printf("mgmt: Warning: Overwriting handler for command: %s", lowerCommand)
	}
	s.handlers[lowerCommand] = CommandInfo{ // <-- Store CommandInfo
		Handler:     handler,
		Description: description,
	}
	log.Printf("mgmt: Registered handler for command '%s': %s", lowerCommand, description)
}

// Start listening on the Unix socket.
func (s *ManagementServer) Start() error {
	// Ensure the quit channel is fresh
	s.quit = make(chan struct{})

	// Remove existing socket file if it exists
	if _, err := os.Stat(s.socketPath); err == nil {
		log.Printf("mgmt: Removing existing socket file: %s", s.socketPath)
		if err := os.Remove(s.socketPath); err != nil {
			// Don't fail hard here, maybe permissions issue, net.Listen will fail later
			log.Printf("mgmt: Warning: Failed to remove existing socket file: %v", err)
		}
	} else if !os.IsNotExist(err) {
		// Error checking the stat, maybe permissions?
		log.Printf("mgmt: Warning: Error checking socket file %s: %v", s.socketPath, err)
	}

	// Create the listener
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket %s: %w", s.socketPath, err)
	}
	s.listener = listener
	// Make socket user-RW only for security
	if err := os.Chmod(s.socketPath, 0600); err != nil {
		log.Printf("mgmt: Warning: could not set socket permissions: %v", err)
	}

	log.Printf("mgmt: management server listening on %s", s.socketPath)

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Stop gracefully shuts down the management server.
func (s *ManagementServer) Stop() {
	log.Printf("mgmt:  Stopping management server...")
	close(s.quit) // Signal acceptLoop to stop

	if s.listener != nil {
		// Closing the listener will cause Accept() to return an error, breaking the loop
		s.listener.Close()
	}

	s.wg.Wait() // Wait for acceptLoop and connection handlers to finish

	// Clean up the socket file
	if _, err := os.Stat(s.socketPath); err == nil {
		if err := os.Remove(s.socketPath); err != nil {
			log.Printf("mgmt: Error removing socket file %s: %v", s.socketPath, err)
		} else {
			log.Printf("mgmt: Removed socket file: %s", s.socketPath)
		}
	}

	log.Printf("mgmt: server stopped.")
}

// acceptLoop waits for incoming connections.
func (s *ManagementServer) acceptLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.quit:
			log.Printf("mgmt: Accept loop received quit signal.")
			return
		default:
			// Set a deadline on accept to periodically check quit channel
			if unixListener, ok := s.listener.(*net.UnixListener); ok {
				_ = unixListener.SetDeadline(time.Now().Add(1 * time.Second))
			}

			conn, err := s.listener.Accept()
			if err != nil {
				// Check if the error is due to the listener being closed
				if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
					continue // Timeout is expected, continue loop to check quit channel
				}
				// Check if the error is due to the listener being closed deliberately
				select {
				case <-s.quit:
					// We are stopping, this error is expected
					return
				default:
					// Unexpected error
					log.Printf("Error accepting connection: %v", err)
					// Avoid busy-looping on persistent errors
					time.Sleep(100 * time.Millisecond)
					continue // Continue accepting? Or maybe return? For a daemon, probably continue.
				}
			}
			// Handle the connection in a new goroutine
			s.wg.Add(1)
			go s.handleConnection(conn)
		}
	}
}

// handleConnection processes commands from a single client connection.
func (s *ManagementServer) handleConnection(conn net.Conn) {
	// ... (Setup remains the same) ...
	defer s.wg.Done()
	defer conn.Close()
	remoteAddr := conn.RemoteAddr().String() // Get address once for logging

	log.Printf("mgmt: client connected: %s", remoteAddr)

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	// --- Authentication Check ---
	if s.password != "" {
		// Set a short deadline for receiving the password
		conn.SetReadDeadline(time.Now().Add(5 * time.Second)) // Short timeout for auth

		// Expect password as the first line
		clientPass, err := reader.ReadString('\n')
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("mgmt: authentication timeout for %s", remoteAddr)
				fmt.Fprintln(writer, ".")
			} else {
				log.Printf("mgmt: error reading password from %s: %v", remoteAddr, err)
				fmt.Fprintln(writer, ".")
			}
			writer.Flush()
			time.Sleep(2000 * time.Millisecond)
			return // Disconnect on error/timeout
		}

		// Reset deadline after successful read
		conn.SetReadDeadline(time.Time{})

		clientPass = strings.TrimSpace(clientPass)
		if clientPass != s.password {
			log.Printf("mgmt: authentication failed for %s", remoteAddr)
			fmt.Fprintln(writer, ".")
			writer.Flush()
			// Optional: Add a small delay to slow down brute-force attempts
			time.Sleep(2000 * time.Millisecond)
			return // Disconnect on wrong password
		}
		// Password is correct
		log.Printf("mgmt: client authenticated successfully: %s", remoteAddr)
		// Send confirmation (optional but good practice)
		// fmt.Fprintln(writer, "OK: Authenticated") // Client needs to handle this extra line
		// writer.Flush() // Ensure confirmation is sent before proceeding
		// For simplicity in the client, we might skip sending the OK back for auth

	} else {
		// No password configured on server, connection allowed directly
		log.Printf("mgmt: client connected (no auth required): %s", remoteAddr)
	}
	for {
		// ... (Read deadline and read logic remains the same) ...
		// Set a read deadline
		conn.SetReadDeadline(time.Now().Add(30 * time.Second)) // Adjust timeout as needed

		// Read command line
		cmdLine, err := reader.ReadString('\n')
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("mgmt: client read timeout: %s", conn.RemoteAddr().String())
				fmt.Fprintln(writer, "error: Read timeout") // Inform client
				writer.Flush()
				return // Close connection on timeout
			}
			// Handle EOF (client closed connection) or other errors
			if err.Error() != "EOF" { // Don't log EOF as an error
				log.Printf("mgmt: error reading command from %s: %v", conn.RemoteAddr().String(), err)
			} else {
				log.Printf("mgmt: client disconnected: %s", conn.RemoteAddr().String())
			}
			return // Close connection
		}

		// Reset read deadline after successful read
		conn.SetReadDeadline(time.Time{})

		cmdLine = strings.TrimSpace(cmdLine)
		if cmdLine == "" {
			continue // Ignore empty lines
		}
		if cmdLine == "quit" { // Special command for client to disconnect cleanly
			log.Printf("mgmt: client %s requested quit.", conn.RemoteAddr().String())
			fmt.Fprintln(writer, "OK: Bye!")
			writer.Flush()
			return
		}

		parts := strings.Fields(cmdLine)
		command := strings.ToLower(parts[0]) // Ensure command lookup is case-insensitive
		args := parts[1:]

		var response string
		var handlerErr error

		s.mu.RLock()
		// Retrieve CommandInfo struct
		cmdInfo, ok := s.handlers[command] // <-- Changed to get CommandInfo
		s.mu.RUnlock()

		if ok {
			// Call the handler from the struct
			response, handlerErr = cmdInfo.Handler(args) // <-- Call cmdInfo.Handler
			if handlerErr != nil {
				response = fmt.Sprintf("mgmt: error executing %s: %v", command, handlerErr)
				log.Printf("mgmt: handler error for command '%s': %v", command, handlerErr)
			}
		} else {
			response = fmt.Sprintf("Error: Unknown command '%s'. Try 'help'.", command) // Suggest help
			log.Printf("mgmt: received unknown command: %s", command)
		}

		// ... (Write response logic remains the same) ...
		// Send response back to client
		_, err = writer.WriteString(response + "\n")
		if err != nil {
			log.Printf("mgmt: error writing response to %s: %v", conn.RemoteAddr().String(), err)
			return // Stop if we can't write
		}
		err = writer.Flush()
		if err != nil {
			log.Printf("mgmt: error flushing writer for %s: %v", conn.RemoteAddr().String(), err)
			return // Stop if we can't flush
		}
	}
}

// --- Default Command Handlers ---

func (s *ManagementServer) handleStatusCommand(args []string) (string, error) {
	// ... (Implementation remains the same) ...
	uptime := time.Since(s.startTime).Round(time.Second)
	return fmt.Sprintf("OK: Daemon running. Uptime: %s", uptime), nil
}

func (s *ManagementServer) handlePingCommand(args []string) (string, error) {
	// ... (Implementation remains the same) ...
	return "OK: pong", nil
}

func (s *ManagementServer) handleLogsCommand(args []string) (string, error) {

	processInput := func(reader io.Reader, writer io.Writer) error {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			bytesToWrite := scanner.Bytes()
			_, err := writer.Write(bytesToWrite)
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}

				fmt.Printf("%s\n", bytesToWrite)
			}
		}

		return scanner.Err()
	}

	entries, err := log.GetLastNLogs(20)
	if err != nil {
		return "", err
	}

	var response strings.Builder
	for _, entry := range entries {
		response.WriteString(fmt.Sprintf("%s", entry.LogData))
	}

	res := strings.TrimRight(response.String(), "\n")

	if len(args) > 0 {
		if args[0] == "pretty" {
			var b bytes.Buffer
			r := strings.NewReader(res)
			w := zerolog.ConsoleWriter{Out: &b, TimeFormat: time.RFC3339}
			processInput(r, w)
			return b.String(), nil
		}
	}

	return res, nil
}

// handleHelpCommand lists commands with descriptions or shows help for a specific command.
func (s *ManagementServer) handleHelpCommand(args []string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var response strings.Builder

	if len(args) > 0 {
		// User requested help for a specific command
		cmdName := strings.ToLower(args[0])
		cmdInfo, ok := s.handlers[cmdName]
		if !ok {
			response.WriteString(fmt.Sprintf("Error: Unknown command '%s'. Try 'help' for a list.", cmdName))
		} else {
			// Format the specific help message
			response.WriteString(fmt.Sprintf("OK: Help for '%s':\n", cmdName))
			response.WriteString(fmt.Sprintf("  Usage: %s [args...]\n", cmdName)) // Basic usage assumption
			response.WriteString(fmt.Sprintf("  Description: %s", cmdInfo.Description))
		}
	} else {
		// List all available commands with descriptions
		response.WriteString("OK: Available commands:\n")

		// Get command names and sort them
		cmds := make([]string, 0, len(s.handlers))
		for cmd := range s.handlers {
			cmds = append(cmds, cmd)
		}
		sort.Strings(cmds)

		// Find the longest command name for alignment
		maxLen := 0
		for _, cmd := range cmds {
			if len(cmd) > maxLen {
				maxLen = len(cmd)
			}
		}

		// Build the formatted list
		for _, cmd := range cmds {
			cmdInfo := s.handlers[cmd] // We know it exists
			// Simple padding for alignment
			padding := strings.Repeat(" ", maxLen-len(cmd)+2) // +2 for minimum spacing
			response.WriteString(fmt.Sprintf("  %s%s%s\n", cmd, padding, cmdInfo.Description))
		}
		response.WriteString("\nUse 'help <command>' for more details on a specific command.")
	}

	return strings.TrimRight(response.String(), "\n"), nil
}
