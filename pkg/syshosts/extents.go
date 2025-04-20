package syshosts

import (
	"bytes"
	"net"
	"sort"
)

func (h *Hosts) InsertRaw(lines ...string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, line := range lines {
		// We allow insertion of any line, including empty ones.
		// parseLine handles identification.
		entry := parseLine(line)
		h.entries = append(h.entries, entry)
	}
	return nil // Assuming raw insertion doesn't inherently fail unless parseLine panics (which it shouldn't)
}

// RemoveDuplicateHosts removes duplicate hostnames.
// It first removes duplicates within each line.
// Then, it ensures each hostname appears only once across all active entries,
// keeping the first occurrence encountered. Lines that become empty are removed.
func (h *Hosts) RemoveDuplicateHosts() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Pass 1: Remove duplicates within each line
	for i := range h.entries {
		if !h.entries[i].IsActive || len(h.entries[i].Hostnames) <= 1 {
			continue
		}

		seen := make(map[string]struct{}, len(h.entries[i].Hostnames))
		uniqueHosts := make([]string, 0, len(h.entries[i].Hostnames))
		changed := false
		for _, host := range h.entries[i].Hostnames {
			if _, exists := seen[host]; !exists {
				seen[host] = struct{}{}
				uniqueHosts = append(uniqueHosts, host)
			} else {
				changed = true // Mark that a duplicate was found and removed within this line
			}
		}

		if changed {
			if len(uniqueHosts) == 0 {
				// Mark for removal later if the line becomes empty (shouldn't happen if IsActive is true initially)
				// For now, just update to empty slice. The inter-line pass will handle empty lines.
				h.entries[i].Hostnames = []string{}
			} else {
				h.entries[i].Hostnames = uniqueHosts
			}
			// Update Raw representation if changed
			h.entries[i].Raw = formatEntry(h.entries[i].IP, h.entries[i].Hostnames)
		}
	}

	// Pass 2: Remove duplicate hostnames across different active entries (keep first)
	// Also remove active entries that ended up with no hostnames.
	seenGlobal := make(map[string]bool)
	newEntries := make([]hostEntry, 0, len(h.entries))
	modified := false

	for _, entry := range h.entries {
		if !entry.IsActive {
			newEntries = append(newEntries, entry) // Keep comments/blanks
			continue
		}

		if len(entry.Hostnames) == 0 {
			modified = true // This line needs removal
			continue        // Skip empty active entries
		}

		uniqueHosts := make([]string, 0, len(entry.Hostnames))
		hostRemoved := false
		for _, host := range entry.Hostnames {
			if !seenGlobal[host] {
				uniqueHosts = append(uniqueHosts, host)
				seenGlobal[host] = true
			} else {
				hostRemoved = true // This host was a duplicate from a previous line
			}
		}

		if len(uniqueHosts) == 0 {
			// All hosts on this line were duplicates seen earlier, remove the line
			modified = true
			continue
		}

		if hostRemoved {
			// Some hosts were removed, update the entry
			entry.Hostnames = uniqueHosts
			entry.Raw = formatEntry(entry.IP, entry.Hostnames)
			modified = true
		}
		// Keep the entry (either unchanged or updated)
		newEntries = append(newEntries, entry)

	}

	if modified {
		h.entries = newEntries
	}
}

// WrapLinesAtMaxHostsPerLine reorganizes active entries so that no line
// contains more than 'count' hostnames. Lines exceeding the count are split
// into multiple lines with the same IP. Does nothing if count <= 0.
func (h *Hosts) WrapLinesAtMaxHostsPerLine(count int) {
	if count <= 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	newEntries := make([]hostEntry, 0, len(h.entries)) // Estimate capacity
	modified := false

	for _, entry := range h.entries {
		if !entry.IsActive || len(entry.Hostnames) <= count {
			newEntries = append(newEntries, entry) // Keep as is
			continue
		}

		modified = true
		ip := entry.IP
		allHosts := entry.Hostnames
		start := 0

		// Add the first chunk (modifies the original entry conceptually)
		firstChunk := allHosts[start:min(start+count, len(allHosts))]
		newEntries = append(newEntries, hostEntry{
			IP:        ip,
			Hostnames: firstChunk,
			Raw:       formatEntry(ip, firstChunk),
			IsActive:  true,
		})
		start += count

		// Add subsequent chunks as new entries
		for start < len(allHosts) {
			chunk := allHosts[start:min(start+count, len(allHosts))]
			newEntries = append(newEntries, hostEntry{
				IP:        ip,
				Hostnames: chunk,
				Raw:       formatEntry(ip, chunk),
				IsActive:  true,
			})
			start += count
		}
	}

	if modified {
		h.entries = newEntries
	}
}

// Helper for WrapLinesAtMaxHostsPerLine
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CombineDuplicateIPs merges multiple active entries sharing the same IP address
// into a single entry. Hostnames from the merged entries are combined, and
// duplicates among them are removed. The relative order of non-active entries and
// the first occurrence of an active IP are preserved.
func (h *Hosts) CombineDuplicateIPs() {
	h.mu.Lock()
	defer h.mu.Unlock()

	ipMap := make(map[string][]string)                  // Map IP -> list of all associated hostnames (duplicates included initially)
	seenIPOrder := []string{}                           // Track the order of first appearance for IPs
	ipFirstIndex := make(map[string]int)                // Map IP -> index of its first occurrence in original slice
	keepEntries := make([]hostEntry, 0, len(h.entries)) // Entries to keep (non-active or first active for an IP)
	modified := false

	// Pass 1: Collect hostnames per IP and identify entries to keep/process
	processedIndices := make(map[int]bool) // Track indices already handled (e.g., non-active)
	for i, entry := range h.entries {
		if !entry.IsActive {
			keepEntries = append(keepEntries, entry)
			processedIndices[i] = true
			continue
		}

		if _, seen := ipMap[entry.IP]; !seen {
			// First time seeing this IP
			seenIPOrder = append(seenIPOrder, entry.IP)
			ipFirstIndex[entry.IP] = i
			ipMap[entry.IP] = make([]string, 0)
			keepEntries = append(keepEntries, entry) // Keep placeholder for first occurrence
			processedIndices[i] = true
		} else {
			modified = true // Will need to merge this entry later
		}
		// Append all hostnames (will dedup later)
		ipMap[entry.IP] = append(ipMap[entry.IP], entry.Hostnames...)
	}

	if !modified {
		return // No duplicate IPs found
	}

	// Pass 2: Create final combined entries and update the keepEntries list
	finalEntries := make([]hostEntry, 0, len(keepEntries))
	originalIndexOffset := 0 // Adjust index lookup for removed items
	for _, keptEntry := range keepEntries {
		if !keptEntry.IsActive {
			finalEntries = append(finalEntries, keptEntry)
			originalIndexOffset++
			continue
		}

		ip := keptEntry.IP
		allHostsForIP := ipMap[ip]

		// Deduplicate hostnames for this IP
		seenHosts := make(map[string]struct{}, len(allHostsForIP))
		uniqueHosts := make([]string, 0, len(allHostsForIP))
		for _, host := range allHostsForIP {
			if _, exists := seenHosts[host]; !exists {
				seenHosts[host] = struct{}{}
				uniqueHosts = append(uniqueHosts, host)
			}
		}

		// Create the final combined entry
		combinedEntry := hostEntry{
			IP:        ip,
			Hostnames: uniqueHosts,
			Raw:       formatEntry(ip, uniqueHosts),
			IsActive:  true,
		}
		finalEntries = append(finalEntries, combinedEntry)

	}

	h.entries = finalEntries
}

// Clean applies a standard set of cleaning operations:
// 1. Combines duplicate IP entries.
// 2. Removes duplicate hostnames (within lines and across lines).
// Ensures the hosts data is in a normalized state.
func (h *Hosts) Clean() {
	h.mu.Lock() // Lock for the entire sequence
	defer h.mu.Unlock()

	// Call internal (non-locking) versions if they exist, or just call the public ones.
	// Since the public methods handle locking, calling them sequentially is safe here.
	h.combineDuplicateIPsInternal()  // Assumes internal version exists without locking
	h.removeDuplicateHostsInternal() // Assumes internal version exists without locking
	// Optional: Add removal of active lines that are truly empty, if not fully covered above
	h.removeEmptyActiveEntriesInternal()
}

// Internal helper for CombineDuplicateIPs without locking (for use in Clean)
func (h *Hosts) combineDuplicateIPsInternal() {
	// --- SBE --- Copy logic from CombineDuplicateIPs --- SBE ---
	ipMap := make(map[string][]string)
	seenIPOrder := []string{}
	ipFirstIndex := make(map[string]int)
	keepEntries := make([]hostEntry, 0, len(h.entries))
	modified := false
	processedIndices := make(map[int]bool)

	for i, entry := range h.entries {
		if !entry.IsActive {
			keepEntries = append(keepEntries, entry)
			processedIndices[i] = true
			continue
		}
		if _, seen := ipMap[entry.IP]; !seen {
			seenIPOrder = append(seenIPOrder, entry.IP)
			ipFirstIndex[entry.IP] = i
			ipMap[entry.IP] = make([]string, 0)
			keepEntries = append(keepEntries, entry)
			processedIndices[i] = true
		} else {
			modified = true
		}
		ipMap[entry.IP] = append(ipMap[entry.IP], entry.Hostnames...)
	}

	if !modified {
		return
	}

	finalEntries := make([]hostEntry, 0, len(keepEntries))
	for _, keptEntry := range keepEntries {
		if !keptEntry.IsActive {
			finalEntries = append(finalEntries, keptEntry)
			continue
		}
		ip := keptEntry.IP
		allHostsForIP := ipMap[ip]
		seenHosts := make(map[string]struct{}, len(allHostsForIP))
		uniqueHosts := make([]string, 0, len(allHostsForIP))
		for _, host := range allHostsForIP {
			if _, exists := seenHosts[host]; !exists {
				seenHosts[host] = struct{}{}
				uniqueHosts = append(uniqueHosts, host)
			}
		}
		combinedEntry := hostEntry{
			IP: ip, Hostnames: uniqueHosts, Raw: formatEntry(ip, uniqueHosts), IsActive: true,
		}
		finalEntries = append(finalEntries, combinedEntry)
	}
	h.entries = finalEntries
	// --- SBE --- End Copy --- SBE ---
}

// Internal helper for RemoveDuplicateHosts without locking (for use in Clean)
func (h *Hosts) removeDuplicateHostsInternal() {
	// --- SBE --- Copy logic from RemoveDuplicateHosts --- SBE ---
	for i := range h.entries {
		if !h.entries[i].IsActive || len(h.entries[i].Hostnames) <= 1 {
			continue
		}
		seen := make(map[string]struct{}, len(h.entries[i].Hostnames))
		uniqueHosts := make([]string, 0, len(h.entries[i].Hostnames))
		changed := false
		for _, host := range h.entries[i].Hostnames {
			if _, exists := seen[host]; !exists {
				seen[host] = struct{}{}
				uniqueHosts = append(uniqueHosts, host)
			} else {
				changed = true
			}
		}
		if changed {
			if len(uniqueHosts) == 0 {
				h.entries[i].Hostnames = []string{}
			} else {
				h.entries[i].Hostnames = uniqueHosts
			}
			h.entries[i].Raw = formatEntry(h.entries[i].IP, h.entries[i].Hostnames)
		}
	}

	seenGlobal := make(map[string]bool)
	newEntries := make([]hostEntry, 0, len(h.entries))
	modified := false
	for _, entry := range h.entries {
		if !entry.IsActive {
			newEntries = append(newEntries, entry)
			continue
		}
		if len(entry.Hostnames) == 0 {
			modified = true
			continue
		}
		uniqueHosts := make([]string, 0, len(entry.Hostnames))
		hostRemoved := false
		for _, host := range entry.Hostnames {
			if !seenGlobal[host] {
				uniqueHosts = append(uniqueHosts, host)
				seenGlobal[host] = true
			} else {
				hostRemoved = true
			}
		}
		if len(uniqueHosts) == 0 {
			modified = true
			continue
		}
		if hostRemoved {
			entry.Hostnames = uniqueHosts
			entry.Raw = formatEntry(entry.IP, entry.Hostnames)
			modified = true
		}
		newEntries = append(newEntries, entry)
	}
	if modified {
		h.entries = newEntries
	}
	// --- SBE --- End Copy --- SBE ---
}

// Internal helper to remove active entries with no hostnames
func (h *Hosts) removeEmptyActiveEntriesInternal() {
	newEntries := make([]hostEntry, 0, len(h.entries))
	modified := false
	for _, entry := range h.entries {
		if entry.IsActive && len(entry.Hostnames) == 0 {
			modified = true // Mark that an empty active entry was found and skipped
			continue
		}
		newEntries = append(newEntries, entry)
	}
	if modified {
		h.entries = newEntries
	}
}

// SortHosts sorts the hostnames alphabetically within each active entry.
// The Raw representation of modified entries is updated.
func (h *Hosts) SortHosts() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	modified := false
	for i := range h.entries {
		if h.entries[i].IsActive && len(h.entries[i].Hostnames) > 1 {
			// Check if already sorted to avoid unnecessary work/flagging modification
			if !sort.StringsAreSorted(h.entries[i].Hostnames) {
				sort.Strings(h.entries[i].Hostnames)
				h.entries[i].Raw = formatEntry(h.entries[i].IP, h.entries[i].Hostnames)
				modified = true // We only care if *any* line was modified, but fine-tuning is possible
			}
		}
	}
	return modified
	// 'modified' flag isn't strictly necessary for this func's logic but could be useful if needed later.
}

// SortIPs sorts the active host entries based on their IP address.
// IPv4 addresses come before IPv6 addresses. Within the same family, standard
// IP comparison is used. Non-active entries (comments, blanks) are kept together
// at the beginning of the file in their original relative order.
func (h *Hosts) SortIPs() {
	h.mu.Lock()
	defer h.mu.Unlock()

	activeEntries := make([]hostEntry, 0, len(h.entries))
	nonActiveEntries := make([]hostEntry, 0, len(h.entries))

	// Separate active and non-active entries
	for _, entry := range h.entries {
		if entry.IsActive {
			activeEntries = append(activeEntries, entry)
		} else {
			nonActiveEntries = append(nonActiveEntries, entry)
		}
	}

	// Sort active entries using custom IP comparison
	sort.SliceStable(activeEntries, func(i, j int) bool {
		return compareIPStrings(activeEntries[i].IP, activeEntries[j].IP) < 0
	})

	// Combine non-active first, then sorted active
	h.entries = append(nonActiveEntries, activeEntries...)
	// Note: If preserving original relative positions of comments interspersed
	// with active lines is desired, the sorting logic becomes significantly more complex.
	// This implementation groups all comments/blanks at the top.
}

// compareIPStrings compares two IP address strings.
// It returns:
//
//	-1 if ipStr1 < ipStr2
//	 0 if ipStr1 == ipStr2
//	 1 if ipStr1 > ipStr2
//
// IPv4 addresses are considered less than IPv6 addresses.
// Unparsable IPs might be treated as less than parsable ones or handled differently.
func compareIPStrings(ipStr1, ipStr2 string) int {
	ip1 := net.ParseIP(ipStr1)
	ip2 := net.ParseIP(ipStr2)

	// Handle nil cases (invalid IPs) - place them perhaps at the beginning or end?
	// Let's place invalid IPs before valid ones.
	if ip1 == nil && ip2 == nil {
		return 0
	} // Both invalid, treat as equal
	if ip1 == nil {
		return -1
	} // Invalid < Valid
	if ip2 == nil {
		return 1
	} // Valid > Invalid

	// Check IP family (IPv4 vs IPv6)
	is4_1 := ip1.To4() != nil
	is4_2 := ip2.To4() != nil

	if is4_1 && !is4_2 {
		return -1
	} // IPv4 < IPv6
	if !is4_1 && is4_2 {
		return 1
	} // IPv6 > IPv4

	// Both are of the same family (either both IPv4 or both IPv6)
	// Compare byte-wise
	return bytes.Compare(ip1, ip2)
}

// --- Helper parseLine (assuming it exists from previous implementation) ---
/*
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
	// Preserve original Raw line for comments/formatting
	// If we wanted to *always* format active lines cleanly:
	// entry.Raw = formatEntry(entry.IP, entry.Hostnames)
	return entry
}
*/

// --- Helper formatEntry (assuming it exists from previous implementation) ---
/*
func formatEntry(ip string, hostnames []string) string {
	// Use tab as primary separator, space for subsequent hosts
	return fmt.Sprintf("%s\t%s", ip, strings.Join(hostnames, " "))
}
*/

// --- String() method (assuming it exists from previous implementation) ---
/*
func (h *Hosts) String() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	lines := make([]string, len(h.entries))
	for i, entry := range h.entries {
		lines[i] = entry.Raw
	}
	output := strings.Join(lines, "\n")
	if len(h.entries) > 0 && !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	return output
}
*/
