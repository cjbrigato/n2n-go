package management

import (
	"bufio" // <-- Import errors
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	connectTimeout = 1 * time.Second
	// Slightly longer overall timeout to accommodate potential auth roundtrip + command
	readWriteTimeout = 8 * time.Second
	authTimeout      = 3 * time.Second // Specific timeout for sending password
)

type ManagementClient struct {
	socketPath string
	password   string
}

func NewManagementClient(app string, password string) *ManagementClient {
	c := &ManagementClient{
		socketPath: GetDefaultSocketPath(app),
		password:   password,
	}
	return c
}

func (c *ManagementClient) IsManagementServerStarted() bool {
	res, err := c.SendCommand("ping")
	if err != nil {
		return false
	}
	if res != pongString {
		return false
	}
	return true
}

func (c *ManagementClient) SendCommand(command string) (string, error) {

	if command == "" {
		command = "help"
	}

	conn, err := net.DialTimeout("unix", c.socketPath, connectTimeout)
	if err != nil {
		return "", fmt.Errorf("Error connecting to daemon socket %s: %v\nIs the daemon running?", c.socketPath, err)
	}
	defer conn.Close()
	if c.password != "" {
		if err := conn.SetWriteDeadline(time.Now().Add(authTimeout)); err != nil {
			return "", fmt.Errorf("Error setting write deadline for auth: %v", err)
		}
		_, err = fmt.Fprintf(conn, "%s\n", c.password) // Send password + newline
		if err != nil {
			return "", fmt.Errorf("Error sending password: %v", err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(authTimeout)); err != nil {
			return "", fmt.Errorf("Error setting read deadline for auth: %v", err)
		}
		reader := bufio.NewReader(conn)
		response, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("Error reading auth response: %v", err)
		}
		if strings.Contains(response, nokAuthString) {
			return "", fmt.Errorf("auth failure: %s", strings.TrimSpace(response))
		}
	}
	deadline := time.Now().Add(readWriteTimeout)
	if err := conn.SetDeadline(deadline); err != nil { // Sets both read and write deadline
		return "", fmt.Errorf("Error setting read/write deadline: %v", err)
	}

	_, err = fmt.Fprintf(conn, "%s\n", command) // Send command + newline
	if err != nil {
		// This might be the point where failure occurs if auth was required but failed/missing
		return "", fmt.Errorf("Error sending command (authentication might have failed): %v", err)
	}

	reader := bufio.NewReader(conn)
	response, err := recvMessage(reader)

	// Read the response
	/*reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	*/
	if err != nil {
		return "", fmt.Errorf("Error reading response: %v", err)
	}

	return strings.TrimSpace(string(response)), nil

	// Print the response
	/*fmt.Print(strings.TrimSpace(response))
	if !strings.HasSuffix(response, "\n") {
		fmt.Println()
	}*/
}
