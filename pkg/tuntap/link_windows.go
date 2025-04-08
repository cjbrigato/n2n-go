//go:build windows

package tuntap

import (
	"errors"
	"fmt"
	"net"
	"unsafe" // Needed for pointer manipulation for IPv4

	"n2n-go/pkg/log"

	"golang.org/x/sys/windows"
)

// --- Constants ---
const DefaultMTU = 1420
const (
	AF_INET  = windows.AF_INET
	AF_INET6 = windows.AF_INET6
)

// --- Load DLL and Procedures (Unchanged) ---
var (
	iphlpapi                        = windows.NewLazySystemDLL("iphlpapi.dll")
	procCreateUnicastIpAddressEntry = iphlpapi.NewProc("CreateUnicastIpAddressEntry")
	procDeleteUnicastIpAddressEntry = iphlpapi.NewProc("DeleteUnicastIpAddressEntry")
	procGetIpInterfaceEntry         = iphlpapi.NewProc("GetIpInterfaceEntry")
	procSetIpInterfaceEntry         = iphlpapi.NewProc("SetIpInterfaceEntry")
)

// --- Struct Definitions ---
// REMOVED - Using structs from golang.org/x/sys/windows, assuming Address is RawSockaddrInet6

// --- callProcErr Helper (With r1 logging) ---
func callProcErr(proc *windows.LazyProc, args ...uintptr) error {
	r1, _, errno := proc.Call(args...)
	if errno != windows.ERROR_SUCCESS {
		return error(errno)
	}
	if r1 != 0 {
		log.Printf("Warning: proc.Call returned r1=%d errno=0.", r1)
	}
	return nil
}

// --- Helper Functions using loaded procedures ---

// setIPAddress treats row.Address as RawSockaddrInet6 and uses unsafe for IPv4.
func setIPAddress(ifIndex uint32, ipCIDR string) error {
	ip, ipNet, err := net.ParseCIDR(ipCIDR)
	if err != nil {
		return fmt.Errorf("parse CIDR %q: %w", ipCIDR, err)
	}
	prefixLen, _ := ipNet.Mask.Size()

	// Assume windows.MibUnicastIpAddressRow.Address is RawSockaddrInet6
	row := windows.MibUnicastIpAddressRow{
		InterfaceIndex:     ifIndex,
		OnLinkPrefixLength: uint8(prefixLen),
		SkipAsSource:       0, // uint8
		DadState:           windows.IpDadStatePreferred,
		PrefixOrigin:       windows.IpPrefixOriginManual,
		SuffixOrigin:       windows.IpSuffixOriginManual,
		ValidLifetime:      0xFFFFFFFF,
		PreferredLifetime:  0xFFFFFFFF,
		// Address field will be populated below
	}

	if ip4 := ip.To4(); ip4 != nil {
		// --- Handle IPv4 using unsafe pointer copy ---
		// Create the source IPv4 structure
		addrV4 := windows.RawSockaddrInet4{
			Family: windows.AF_INET,
			// Port: 0, // Zero is default
		}
		copy(addrV4.Addr[:], ip4)

		// Get size and pointers
		sizeV4 := unsafe.Sizeof(addrV4)
		destPtr := unsafe.Pointer(&row.Address) // Pointer to the RawSockaddrInet6 field
		srcPtr := unsafe.Pointer(&addrV4)       // Pointer to the temporary RawSockaddrInet4

		// Ensure we don't write past the destination field's size (though v4 is smaller than v6)
		if sizeV4 > unsafe.Sizeof(row.Address) {
			return fmt.Errorf("internal error: sizeof(RawSockaddrInet4) > sizeof(RawSockaddrInet6)")
		}

		// Copy the bytes from addrV4 into the memory space of row.Address
		// The Family field (first 2 bytes) will be set to AF_INET.
		copy((*(*[1 << 30]byte)(destPtr))[:sizeV4], (*(*[1 << 30]byte)(srcPtr))[:sizeV4])

		// Ensure remaining bytes (if any) in the destination struct are zero? Might not be necessary.

	} else if ip6 := ip.To16(); ip6 != nil {
		// --- Handle IPv6 directly ---
		row.Address.Family = windows.AF_INET6
		// row.Address.Port = 0 // Default
		// row.Address.Flowinfo = 0 // Default
		copy(row.Address.Addr[:], ip6) // Use the .Addr field directly

		// Handle ScopeId - set in both the address struct AND the main MIB row
		// if ip.IsLinkLocalUnicast() { row.Address.Scope_id = ifIndex } // Needs correct zone index
		row.Address.Scope_id = 0           // Default to 0 if not link-local or zone unknown
		row.ScopeId = row.Address.Scope_id // Copy to the main struct field

	} else {
		return fmt.Errorf("invalid IP format: %s", ip.String())
	}

	log.Printf("Attempting CreateUnicastIpAddressEntry call for %s on IfIndex %d", ipCIDR, ifIndex)
	err = callProcErr(procCreateUnicastIpAddressEntry, uintptr(unsafe.Pointer(&row)))
	if err != nil {
		if errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
			log.Printf("IP address %s already exists on IfIndex %d.", ipCIDR, ifIndex)
			return nil
		}
		return fmt.Errorf("CreateUnicastIpAddressEntry call failed: %w", err)
	}
	log.Printf("Successfully added IP address %s to IfIndex %d", ipCIDR, ifIndex)
	return nil
}

// setMTUAndEnable uses windows.MibIpInterfaceRow (assuming .Enabled is uint8)
func setMTUAndEnable(ifIndex uint32, family uint16, mtu int) error {
	// Use the correct windows struct name
	keyRow := windows.MibIpInterfaceRow{Family: family, InterfaceIndex: ifIndex}
	log.Printf("Getting IP interface entry for IfIndex %d (Family %d)...", ifIndex, family)

	err := callProcErr(procGetIpInterfaceEntry, uintptr(unsafe.Pointer(&keyRow)))
	if err != nil {
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			log.Printf("Warning: GetIpInterfaceEntry not found IfIndex %d (Family %d). %v", ifIndex, family, err)
			return fmt.Errorf("cannot get interface entry: %w", err)
		}
		return fmt.Errorf("GetIpInterfaceEntry call failed: %w", err)
	}

	modifiedRow := keyRow
	changed := false
	if mtu > 0 {
		currentMtu := modifiedRow.NlMtu
		if currentMtu != uint32(mtu) {
			modifiedRow.NlMtu = uint32(mtu)
			log.Printf("Setting MTU to %d for IfIndex %d (Family %d) (was %d)", mtu, ifIndex, family, currentMtu)
			changed = true
		}
	}

	// Access .Enabled field as uint8, check against 0
	/*if modifiedRow.Enabled == 0 { // Check if currently disabled
		modifiedRow.Enabled = 1; // Set to 1 (true)
		log.Printf("Setting interface IfIndex %d (Family %d) to Enabled", ifIndex, family); changed = true
	}*/

	if changed {
		log.Printf("Applying SetIpInterfaceEntry call for IfIndex %d (Family %d)", ifIndex, family)
		err = callProcErr(procSetIpInterfaceEntry, uintptr(unsafe.Pointer(&modifiedRow)))
		if err != nil {
			return fmt.Errorf("SetIpInterfaceEntry call failed: %w", err)
		}
		log.Printf("Successfully applied SetIpInterfaceEntry call for IfIndex %d (Family %d)", ifIndex, family)
	} else {
		log.Printf("No changes needed for MTU/Enable status for IfIndex %d (Family %d)", ifIndex, family)
	}
	return nil
}

// --- Interface Methods (unchanged logic, using helpers above) ---
func (i *Interface) ConfigureInterface(macAddr, ipCIDR string, mtu int) error { /* ... as before ... */
	if i.Iface == nil {
		return fmt.Errorf("underlying device is nil")
	}
	ifIndex := i.GetIfIndex()
	if ifIndex == 0 {
		return fmt.Errorf("IfIndex is 0")
	}
	log.Printf("Configuring Windows TAP interface IfIndex: %d", ifIndex)
	if macAddr != "" {
		actualMac := i.HardwareAddr()
		log.Printf("Note: Desired MAC (%s) set via registry; current is %s", macAddr, actualMac.String())
	}
	err := setIPAddress(ifIndex, ipCIDR)
	if err != nil {
		return fmt.Errorf("set IP address failed: %w", err)
	}
	ip, _, err := net.ParseCIDR(ipCIDR)
	if err != nil {
		return fmt.Errorf("internal re-parse CIDR %s: %w", ipCIDR, err)
	}
	family := uint16(AF_INET)
	if ip.To4() == nil && ip.To16() != nil {
		family = AF_INET6
	}
	if mtu <= 0 {
		mtu = DefaultMTU
	}
	err = setMTUAndEnable(ifIndex, family, mtu)
	if err != nil {
		log.Printf("Warning: Failed set MTU/Enable status: %v", err)
	}
	log.Printf("Windows interface configuration finished for IfIndex %d", ifIndex)
	return nil
}
func (i *Interface) IfUp(ipCIDR string) error { return i.ConfigureInterface("", ipCIDR, DefaultMTU) }
func (i *Interface) IfMac(macAddr string) error { /* ... as before ... */
	if i.Iface == nil {
		return fmt.Errorf("underlying device is nil")
	}
	log.Printf("Note: Setting MAC address (%s) handled via registry.", macAddr)
	actualMac := i.HardwareAddr()
	ifIndex := i.GetIfIndex()
	if actualMac != nil {
		log.Printf("Current MAC for IfIndex %d is %s", ifIndex, actualMac.String())
	} else {
		log.Printf("Warning: Could not get current MAC for IfIndex %d", ifIndex)
	}
	return nil
}
