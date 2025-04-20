// Package hostsfile provides utilities to read, manipulate, and write the system's hosts file.
package syshosts

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
)

const (
	// Default hosts file path for Unix-like systems.
	hostsFilePathDefault = "/etc/hosts"
	// Default hosts file path for Windows.
	hostsFilePathWindows = `C:\Windows\System32\drivers\etc\hosts`
)

var (
	// ErrInvalidIP is returned when an invalid IP address format is provided.
	ErrInvalidIP = errors.New("invalid IP address format")
	// ErrHostnameNotFound is returned when a specific hostname is not found for removal.
	ErrHostnameNotFound = errors.New("hostname not found")
	// ErrIPNotFound is returned when a specific IP address is not found for removal.
	ErrIPNotFound = errors.New("ip address not found")
	// ErrHostEntryNotFound is returned when a specific IP/hostname pair is not found for removal.
	ErrHostEntryNotFound = errors.New("host entry (ip/hostname pair) not found")
)

// hostEntry represents a single line in the hosts file.
type hostEntry struct {
	IP        string   // IP address if it's an active entry
	Hostnames []string // Hostnames if it's an active entry
	Raw       string   // The original raw line from the file
	IsActive  bool     // True if it's a valid IP/hostname line, false for comments/blanks/malformed
}

// Hosts represents the system's hosts file content and provides methods to manipulate it.
type Hosts struct {
	filePath string
	entries  []hostEntry
	mu       sync.RWMutex // Protects access to entries
}

// getHostsFilePath returns the conventional path to the hosts file for the current OS.
func getHostsFilePath() string {
	if runtime.GOOS == "windows" {
		return hostsFilePathWindows
	}
	return hostsFilePathDefault
}

// loadAndParse reads the specified hosts file, parses its content,
// and returns the slice of host entries or an error.
// This is the internal core logic used by NewHosts and Reload.
func loadAndParse(filePath string) ([]hostEntry, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read hosts file '%s': %w", filePath, err)
	}

	entries := make([]hostEntry, 0) // Initialize slice

	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := scanner.Text()
		entry := parseLine(line) // Use the existing parser
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning hosts file content: %w", err)
	}

	return entries, nil
}

// NewHosts creates a new Hosts object by loading and parsing the system's hosts file.
func NewHosts() (*Hosts, error) {
	filePath := getHostsFilePath() // Determine the path

	// Use the shared loading and parsing logic
	entries, err := loadAndParse(filePath)
	if err != nil {
		return nil, err // Error already formatted by loadAndParse
	}

	// Create and initialize the Hosts struct
	h := &Hosts{
		filePath: filePath,
		entries:  entries,
		// mu is initialized automatically (zero value is usable)
	}

	return h, nil
}

// parseLine parses a single line from the hosts file.
func parseLine(line string) hostEntry {
	trimmedLine := strings.TrimSpace(line)
	entry := hostEntry{Raw: line, IsActive: false} // Default to inactive/comment

	if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") {
		return entry // Blank line or comment
	}

	fields := strings.Fields(trimmedLine)
	if len(fields) < 2 {
		return entry // Malformed line (IP without host or junk)
	}

	ipStr := fields[0]
	if net.ParseIP(ipStr) == nil {
		// If the first field isn't a valid IP, treat the line as non-active (maybe malformed comment)
		return entry
	}

	// If it looks like a valid entry
	entry.IsActive = true
	entry.IP = ipStr
	entry.Hostnames = fields[1:]

	// Reconstruct raw line without potential leading/trailing space from original if needed
	// Or just keep the original raw line? Keeping original Raw is better for preserving comments formatting.
	// Let's keep the original Raw line.

	return entry
}

// formatEntry formats an active hostEntry back into a string line.
func formatEntry(ip string, hostnames []string) string {
	return fmt.Sprintf("%s\t%s", ip, strings.Join(hostnames, " "))
}

// Add appends or modifies an entry in the internal representation.
// If the IP already exists, the hostnames are added to that line (avoiding duplicates).
// If the IP does not exist, a new line is added.
// This does not write to the actual file; call Write() for that.
func (h *Hosts) Add(ip string, hosts ...string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if net.ParseIP(ip) == nil {
		return ErrInvalidIP
	}

	if len(hosts) == 0 {
		return errors.New("at least one hostname must be provided")
	}

	// Filter empty or invalid hostnames (basic check)
	validHosts := make([]string, 0, len(hosts))
	for _, host := range hosts {
		hClean := strings.TrimSpace(host)
		if hClean != "" {
			validHosts = append(validHosts, hClean)
		}
	}
	if len(validHosts) == 0 {
		return errors.New("no valid hostnames provided")
	}

	found := false
	for i := range h.entries {
		if h.entries[i].IsActive && h.entries[i].IP == ip {
			existingHostnames := make(map[string]struct{}, len(h.entries[i].Hostnames))
			for _, hn := range h.entries[i].Hostnames {
				existingHostnames[hn] = struct{}{}
			}

			added := false
			for _, host := range validHosts {
				if _, exists := existingHostnames[host]; !exists {
					h.entries[i].Hostnames = append(h.entries[i].Hostnames, host)
					existingHostnames[host] = struct{}{} // Add to map for next iteration
					added = true
				}
			}

			if added {
				// Update the raw representation if hostnames were actually added
				h.entries[i].Raw = formatEntry(h.entries[i].IP, h.entries[i].Hostnames)
			}
			found = true
			break // Assuming only one line per IP for direct modification
		}
	}

	if !found {
		newEntry := hostEntry{
			IP:        ip,
			Hostnames: validHosts,
			IsActive:  true,
			Raw:       formatEntry(ip, validHosts),
		}
		h.entries = append(h.entries, newEntry)
	}

	return nil
}

// IsWritable checks if the hosts file has write permissions for the current user.
func (h *Hosts) IsWritable() bool {
	// Try to open the file in write-only mode. If it succeeds, we have permission.
	file, err := os.OpenFile(h.filePath, os.O_WRONLY|os.O_APPEND, 0660)
	if err != nil {
		// Check specifically for permission errors
		// Note: os.IsPermission might not be perfectly reliable across all OS/filesystem combos
		// but is generally the best built-in approach.
		return os.IsPermission(err) == false // If error is NOT permission, something else is wrong, but technically maybe writable. Let's consider non-permission errors as potentially writable (e.g. file busy?) or handle them more explicitly if needed. A simpler approach is just check if err is nil.
		// return err == nil // Simpler: writable only if open succeeds without error. Let's use this.
	}
	// If open succeeded, close immediately and return true.
	file.Close()
	return true

	/* More robust permission check (might be needed depending on how os.OpenFile behaves): */
	/*
		    _, err := os.Stat(h.filePath)
			if err != nil {
				// File doesn't exist or other stat error, cannot determine writability.
				return false
			}
			// Try opening for writing
			file, err := os.OpenFile(h.filePath, os.O_WRONLY|os.O_APPEND, 0) // Use 0 for mode when just checking
			if err == nil {
				file.Close()
				return true // Open succeeded
			}
			// If open failed, check if it was specifically a permission error
			if os.IsPermission(err) {
				return false // Explicit permission denied
			}
			// Other error (e.g., file busy, filesystem error). Might technically be writable later,
			// but for a simple check, consider it not writable now.
			return false
	*/
}

// Write saves the current in-memory representation back to the hosts file.
// It requires appropriate permissions.
func (h *Hosts) Write() error {
	h.mu.RLock() // Use RLock for reading the entries slice
	defer h.mu.RUnlock()

	lines := make([]string, len(h.entries))
	for i, entry := range h.entries {
		lines[i] = entry.Raw
	}
	output := strings.Join(lines, "\n")
	// Add trailing newline if the original file likely had one or if it's standard practice
	if !strings.HasSuffix(output, "\n") {
		output += "\n"
	}

	// WriteFile truncates and writes, which is usually desired for hosts file.
	// It often handles atomic writes (write to temp file, then rename).
	// Use standard permissions like 0644.
	err := os.WriteFile(h.filePath, []byte(output), 0644)
	if err != nil {
		return fmt.Errorf("failed to write hosts file '%s': %w", h.filePath, err)
	}
	return nil
}

// Has checks if a specific IP/hostname pair exists in the active entries.
func (h *Hosts) Has(ip string, host string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}

	for _, entry := range h.entries {
		if entry.IsActive && entry.IP == ip {
			for _, hname := range entry.Hostnames {
				if hname == host {
					return true
				}
			}
		}
	}
	return false
}

// HasHostname checks if a hostname exists in any active entry, regardless of IP.
func (h *Hosts) HasHostname(host string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}

	for _, entry := range h.entries {
		if entry.IsActive {
			for _, hname := range entry.Hostnames {
				if hname == host {
					return true
				}
			}
		}
	}
	return false
}

// HasIP checks if an IP address exists in any active entry.
func (h *Hosts) HasIP(ip string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, entry := range h.entries {
		if entry.IsActive && entry.IP == ip {
			return true
		}
	}
	return false
}

// Remove removes specific hostnames associated with a given IP address from the internal representation.
// If removing the specified hosts leaves the IP entry with no hosts, the entire entry for that IP is removed.
// Returns ErrHostEntryNotFound if the exact ip/host combination doesn't exist.
// Returns ErrInvalidIP if the provided IP is invalid.
func (h *Hosts) Remove(ip string, hosts ...string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if net.ParseIP(ip) == nil {
		return ErrInvalidIP
	}

	if len(hosts) == 0 {
		return errors.New("at least one hostname must be provided for removal")
	}

	hostsToRemove := make(map[string]struct{})
	for _, host := range hosts {
		hClean := strings.TrimSpace(host)
		if hClean != "" {
			hostsToRemove[hClean] = struct{}{}
		}
	}
	if len(hostsToRemove) == 0 {
		return errors.New("no valid hostnames provided for removal")
	}

	foundIP := false
	modified := false
	entryRemoved := false // Flag to track if the entire entry gets removed

	newEntries := make([]hostEntry, 0, len(h.entries))
	for _, entry := range h.entries {
		if entry.IsActive && entry.IP == ip {
			foundIP = true
			originalHostCount := len(entry.Hostnames)
			remainingHostnames := make([]string, 0, len(entry.Hostnames))
			hostWasRemoved := false

			for _, currentHost := range entry.Hostnames {
				if _, shouldRemove := hostsToRemove[currentHost]; !shouldRemove {
					remainingHostnames = append(remainingHostnames, currentHost)
				} else {
					hostWasRemoved = true // Mark that at least one specified host was found and removed
				}
			}

			if !hostWasRemoved {
				// If none of the specified hosts were found on this line, keep the entry as is, but maybe return error later?
				newEntries = append(newEntries, entry)
				continue // Skip to next entry
			}

			modified = true // A modification occurred

			if len(remainingHostnames) == 0 {
				// Remove the entire entry if no hostnames are left
				entryRemoved = true
				continue // Don't add this entry to newEntries
			} else if len(remainingHostnames) < originalHostCount {
				// Update the entry if some hostnames remain and some were removed
				entry.Hostnames = remainingHostnames
				entry.Raw = formatEntry(entry.IP, entry.Hostnames)
				newEntries = append(newEntries, entry)
			} else {
				// Should not happen if hostWasRemoved is true, but defensively keep the entry
				newEntries = append(newEntries, entry)
			}

		} else {
			// Keep non-matching or inactive entries
			newEntries = append(newEntries, entry)
		}
	}

	if !foundIP {
		return ErrIPNotFound // The IP itself was not found
	}
	if !modified {
		// IP was found, but none of the specified hostnames were associated with it
		return ErrHostEntryNotFound
	}
	if entryRemoved {
		h.entries = newEntries
	}
	return nil
}

// RemoveByHostname removes all occurrences of a specific hostname from all active entries.
// If an entry becomes empty after removal, the entire entry is removed.
// Returns ErrHostnameNotFound if the hostname was not found in any active entry.
func (h *Hosts) RemoveByHostname(host string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	host = strings.TrimSpace(host)
	if host == "" {
		return errors.New("hostname cannot be empty")
	}

	found := false
	modified := false
	newEntries := make([]hostEntry, 0, len(h.entries))

	for _, entry := range h.entries {
		if !entry.IsActive {
			newEntries = append(newEntries, entry) // Keep comments/blanks
			continue
		}

		//originalHostCount := len(entry.Hostnames)
		remainingHostnames := make([]string, 0, len(entry.Hostnames))
		hostRemovedFromThisEntry := false

		for _, currentHost := range entry.Hostnames {
			if currentHost == host {
				found = true // Mark that the hostname existed somewhere
				hostRemovedFromThisEntry = true
			} else {
				remainingHostnames = append(remainingHostnames, currentHost)
			}
		}

		if !hostRemovedFromThisEntry {
			// No change to this specific entry
			newEntries = append(newEntries, entry)
		} else {
			modified = true // A modification occurred
			if len(remainingHostnames) == 0 {
				// Remove the entire entry if this was the only hostname
				continue // Don't add to newEntries
			} else {
				// Update the entry with remaining hostnames
				entry.Hostnames = remainingHostnames
				entry.Raw = formatEntry(entry.IP, entry.Hostnames)
				newEntries = append(newEntries, entry)
			}
		}
	}

	if !found {
		return ErrHostnameNotFound
	}

	if modified {
		h.entries = newEntries
	}
	return nil
}

// RemoveByIP removes all active entries associated with the specified IP address.
// Does not return an error if the IP is not found (idempotent).
func (h *Hosts) RemoveByIP(ip string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Quick check if IP is potentially valid syntax, though HasIP would also filter
	// if net.ParseIP(ip) == nil {
	//     // Maybe log a warning or ignore? Silently ignoring for now.
	//	   return
	// }

	newEntries := make([]hostEntry, 0, len(h.entries))
	found := false

	for _, entry := range h.entries {
		if entry.IsActive && entry.IP == ip {
			found = true
			// Skip this entry, effectively removing it
			continue
		}
		// Keep all other entries (inactive or different IP)
		newEntries = append(newEntries, entry)
	}

	// Only replace if something was actually removed to avoid unnecessary slice allocation?
	if found {
		h.entries = newEntries
	}
}

// String returns the string representation of the current in-memory hosts file content.
// This matches the format that would be written by Write().
func (h *Hosts) String() string {
	h.mu.RLock() // Use read lock for accessing entries
	defer h.mu.RUnlock()

	lines := make([]string, len(h.entries))
	for i, entry := range h.entries {
		lines[i] = entry.Raw // Use the stored raw line
	}

	output := strings.Join(lines, "\n")

	// Optionally, ensure a trailing newline, consistent with Write() logic
	if len(h.entries) > 0 && !strings.HasSuffix(output, "\n") {
		output += "\n"
	}

	return output
}

// Reload reads the hosts file from disk again and replaces the current
// in-memory representation with the newly read content.
// Any unwritten changes in the Hosts object will be lost.
func (h *Hosts) Reload() error {
	h.mu.Lock() // Lock for writing to h.entries
	defer h.mu.Unlock()

	// Use the shared loading and parsing logic with the object's stored path
	newEntries, err := loadAndParse(h.filePath)
	if err != nil {
		// Don't discard the old state if reloading fails
		return err // Error already formatted by loadAndParse
	}

	// Replace the old entries with the newly parsed ones
	h.entries = newEntries

	return nil // Reload successful
}
