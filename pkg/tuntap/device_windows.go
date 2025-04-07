//go:build windows

package tuntap

import (
	"errors"
	"fmt"
	"net" // For parsing/formatting MAC
	"os"
	"strings" // For MAC formatting and comparison
	"syscall"
	"time"
	"unsafe" // For pointer ops with GetAdaptersAddresses

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"n2n-go/pkg/log" // Assuming logging package
)

// --- Constants ---
const (
	// Access rights for CreateFile
	GENERIC_READ     = 0x80000000
	GENERIC_WRITE    = 0x40000000
	FILE_SHARE_READ  = 0x00000001
	FILE_SHARE_WRITE = 0x00000002

	// File creation dispositions for CreateFile
	OPEN_EXISTING = 3

	// File attributes and flags for CreateFile
	FILE_ATTRIBUTE_SYSTEM = 0x4
	FILE_FLAG_OVERLAPPED  = 0x40000000 // Recommended for network devices

	// Registry keys and values for TAP adapter discovery and configuration
	networkAdaptersRegKey = `SYSTEM\CurrentControlSet\Control\Class\{4d36e972-e325-11ce-bfc1-08002be10318}`
	componentIDRegValue   = "ComponentId"
	netCfgInstanceIDValue = "NetCfgInstanceID"
	networkAddressValue   = "NetworkAddress" // Standard value name for MAC override

	// IMPORTANT: Use the ComponentId *exactly* as required by the specific installation.
	// User confirmed case-sensitive "TAP0901" for their setup.
	tapWindowsComponentID = "TAP0901"

	// Device path format (using the GUID/NetCfgInstanceID)
	tapDevicePathFormat = `\\.\Global\%s.tap`

	// Address families
	AF_UNSPEC = windows.AF_UNSPEC
	AF_INET   = windows.AF_INET
	AF_INET6  = windows.AF_INET6

	// Flags for GetAdaptersAddresses
	GAA_FLAG_INCLUDE_PREFIX = 0x0010
)

// --- TAP Driver IOCTL Codes (Verify against target driver version) ---
const (
	fileDeviceUnknown = 0x00000022
	methodBuffered    = 0
	fileAnyAccess     = 0
	// Function code for SetMediaStatus IOCTL
	tapIOCTLSetMediaStatus = 6
	// Calculated IOCTL code
	TAP_IOCTL_SET_MEDIA_STATUS = (fileDeviceUnknown << 16) | (fileAnyAccess << 14) | (tapIOCTLSetMediaStatus << 2) | methodBuffered // Should be 0x22C018
)

// --- Struct definitions ---

type DeviceType int

const (
	TUN DeviceType = iota // Note: TUN mode support depends heavily on the driver/config.
	TAP
)

// Config struct - Includes MACAddress for Windows registry setting.
type Config struct {
	Name        string      // Preferred name (used for matching if multiple TAP exist, not currently implemented)
	DevType     DeviceType  // Currently only TAP is reliably supported
	MACAddress  string      // Desired MAC Address (e.g., "00:11:22:AA:BB:CC"), written to registry
	Persist     bool        // Ignored on Windows
	Owner       int         // Ignored on Windows
	Group       int         // Ignored on Windows
	Permissions os.FileMode // Ignored on Windows
}

// Device struct - Holds device state, including discovered IfIndex and MAC on Windows.
type Device struct {
	File    *os.File         // File wrapper around the handle
	handle  windows.Handle   // Raw Windows handle for IOCTLs
	Name    string           // Stores the GUID (NetCfgInstanceID) of the adapter
	DevType DeviceType       // TUN or TAP
	Config  Config           // Original configuration used
	ifIndex uint32           // <<<< ADDED: Stores the discovered interface index
	macAddr net.HardwareAddr // <<<< ADDED: Stores the discovered MAC address
}

// --- Helper Functions ---

// deviceIoControl sends an IOCTL to the TAP device (Corrected version).
func deviceIoControl(handle windows.Handle, ioctl uint32, inBuffer []byte, outBuffer []byte) (uint32, error) {
	var bytesReturned uint32
	var inPtr, outPtr *byte // Use *byte type
	var inSize, outSize uint32
	if len(inBuffer) > 0 {
		inPtr = &inBuffer[0] // Get address directly
		inSize = uint32(len(inBuffer))
	}
	if len(outBuffer) > 0 {
		outPtr = &outBuffer[0] // Get address directly
		outSize = uint32(len(outBuffer))
	}
	err := windows.DeviceIoControl(handle, ioctl, inPtr, inSize, outPtr, outSize, &bytesReturned, nil)
	if err != nil {
		return bytesReturned, fmt.Errorf("DeviceIoControl failed with IOCTL code 0x%X: %w", ioctl, err)
	}
	return bytesReturned, nil
}

// formatMACForRegistry converts standard MAC to registry format (12 hex digits).
func formatMACForRegistry(macStr string) (string, error) {
	hwAddr, err := net.ParseMAC(macStr)
	if err != nil {
		// Check if it's already in 12-digit hex format
		if len(macStr) == 12 {
			isHex := true
			for _, r := range macStr {
				if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
					isHex = false
					break
				}
			}
			if isHex {
				return strings.ToUpper(macStr), nil
			} // Use as is, ensure uppercase
		}
		return "", fmt.Errorf("invalid MAC address format '%s': %w", macStr, err)
	}
	return fmt.Sprintf("%02X%02X%02X%02X%02X%02X", hwAddr[0], hwAddr[1], hwAddr[2], hwAddr[3], hwAddr[4], hwAddr[5]), nil
}

// findInterfaceIndexAndInfoByGUID uses GetAdaptersAddresses to find IfIndex/MAC for a known GUID.
func findInterfaceIndexAndInfoByGUID(guid string) (ifIndex uint32, macAddr net.HardwareAddr, err error) {
	var bufferSize uint32 = 15000 // Start with a reasonable buffer size
	var adapters *windows.IpAdapterAddresses

	for attempt := 0; attempt < 3; attempt++ {
		buffer := make([]byte, bufferSize)
		if len(buffer) == 0 {
			return 0, nil, fmt.Errorf("GetAdaptersAddresses buffer size calculation resulted in zero")
		}
		adapters = (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
		ret := windows.GetAdaptersAddresses(AF_UNSPEC, GAA_FLAG_INCLUDE_PREFIX, 0, adapters, &bufferSize)
		if ret == windows.ERROR_SUCCESS {
			break
		}
		if ret == windows.ERROR_BUFFER_OVERFLOW {
			log.Printf("Debug: GetAdaptersAddresses needs larger buffer (%d bytes), retrying...", bufferSize)
			continue
		}
		return 0, nil, fmt.Errorf("GetAdaptersAddresses call failed with error code: %d", ret)
	}
	if adapters == nil {
		return 0, nil, fmt.Errorf("failed to get adapter addresses structure after successful API call")
	}

	found := false
	for current := adapters; current != nil; current = current.Next {
		// --- CORRECTED AdapterName handling ---
		// AdapterName is a *byte pointing to a C-style string (likely GUID)
		// We need to convert this carefully to a Go string.
		// Iterate byte by byte from the pointer until null terminator or max length.
		var nameBytes []byte
		namePtr := current.AdapterName
		if namePtr != nil {
			// Set a reasonable max length to prevent infinite loops on malformed data
			const maxAdapterNameLen = 256
			for i := 0; i < maxAdapterNameLen; i++ {
				byteVal := *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(namePtr)) + uintptr(i)))
				if byteVal == 0 { // Null terminator found
					break
				}
				nameBytes = append(nameBytes, byteVal)
			}
		}
		adapterName := string(nameBytes) // Convert the collected bytes to a Go string
		// --- End CORRECTED handling ---

		// Compare the extracted adapterName (GUID string) with the target GUID
		if strings.EqualFold(adapterName, guid) { // Case-insensitive compare just in case
			ifIndex = current.IfIndex
			found = true
			// Extract MAC
			if current.PhysicalAddressLength > 0 && len(current.PhysicalAddress) >= int(current.PhysicalAddressLength) {
				macAddr = make(net.HardwareAddr, current.PhysicalAddressLength)
				copy(macAddr, current.PhysicalAddress[:current.PhysicalAddressLength])
				if len(macAddr) > 6 {
					macAddr = macAddr[:6]
				}
			} else {
				log.Printf("Warning: Adapter %s found via API but no physical address reported.", guid)
			}
			break // Found our adapter
		}
	} // End loop adapters

	if !found {
		return 0, nil, fmt.Errorf("adapter with GUID %s not found via GetAdaptersAddresses", guid)
	}
	if ifIndex == 0 {
		return 0, nil, fmt.Errorf("found adapter GUID %s but IfIndex is 0", guid)
	}
	return ifIndex, macAddr, nil
}

// findAndConfigureTapDevice searches registry, sets MAC, finds IfIndex/MAC via API.
// REQUIRES Administrator privileges.
func findAndConfigureTapDevice(config Config) (devPath, instanceID string, ifIdx uint32, currentMAC net.HardwareAddr, err error) {
	targetMAC := ""
	if config.MACAddress != "" {
		targetMAC, err = formatMACForRegistry(config.MACAddress)
		if err != nil {
			return "", "", 0, nil, fmt.Errorf("invalid target MAC address: %w", err)
		}
	}

	// Open base registry key
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, networkAdaptersRegKey, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("failed open network adapters key: %w. Admin needed?", err)
	}
	defer key.Close()

	subkeys, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("failed read adapter subkeys: %w", err)
	}

	foundInRegistry := false
	for _, subkeyName := range subkeys {
		// Determine required access rights
		access := uint32(registry.QUERY_VALUE)
		if targetMAC != "" {
			access |= registry.SET_VALUE
		}

		subkey, errOpen := registry.OpenKey(key, subkeyName, access)
		if errOpen != nil && targetMAC != "" {
			// Try read-only if write failed
			subkey, errOpen = registry.OpenKey(key, subkeyName, registry.QUERY_VALUE)
			if errOpen != nil {
				// Skip keys we cannot open even for read (like 'Properties')
				if !errors.Is(errOpen, windows.ERROR_ACCESS_DENIED) { // Log unexpected errors
					log.Printf("Debug: Skipping registry subkey %s due to unexpected open error: %v", subkeyName, errOpen)
				}
				continue
			}
			log.Printf("Warning: Opened TAP key %s read-only, cannot set MAC %s.", subkeyName, targetMAC)
		} else if errOpen != nil {
			// Failed to open read-only as well
			if !errors.Is(errOpen, windows.ERROR_ACCESS_DENIED) {
				log.Printf("Debug: Skipping registry subkey %s due to unexpected read open error: %v", subkeyName, errOpen)
			}
			continue
		}

		// Read ComponentId to check if it's our TAP adapter
		compID, _, errComp := subkey.GetStringValue(componentIDRegValue)
		// Use exact, case-sensitive comparison as specified by user
		if errComp == nil && compID == tapWindowsComponentID {
			guid, _, errGuid := subkey.GetStringValue(netCfgInstanceIDValue)
			if errGuid != nil {
				log.Printf("Warning: Found TAP adapter '%s' but failed get NetCfgInstanceID: %v", subkeyName, errGuid)
				subkey.Close()
				continue
			}
			instanceID = guid // Found the target GUID
			foundInRegistry = true

			// Set registry MAC if requested and possible
			if targetMAC != "" && (access&registry.SET_VALUE != 0) {
				log.Printf("Writing NetworkAddress=%s to registry key HKLM\\...\\%s for GUID %s", targetMAC, subkeyName, instanceID)
				errSet := subkey.SetStringValue(networkAddressValue, targetMAC)
				if errSet != nil {
					subkey.Close()
					return "", "", 0, nil, fmt.Errorf("failed set NetworkAddress=%s in registry for adapter %s: %w. Admin rights sufficient?", targetMAC, subkeyName, errSet)
				}
				log.Printf("Successfully wrote NetworkAddress to registry for %s.", subkeyName)
			} else if targetMAC != "" {
				log.Printf("Warning: Cannot write NetworkAddress to registry key %s (opened read-only). MAC may not be set.", subkeyName)
			}
			subkey.Close() // Done with this key

			// Now that registry *might* be set, lookup via API to get IfIndex/MAC
			ifIdx, currentMAC, err = findInterfaceIndexAndInfoByGUID(instanceID)
			if err != nil {
				// Maybe registry change needs adapter restart or time? Or API issue?
				log.Printf("Error: Found TAP GUID %s in registry, but failed API lookup: %v", instanceID, err)
				return "", "", 0, nil, fmt.Errorf("found TAP in registry but failed API lookup for GUID %s: %w", instanceID, err)
			}
			devPath = fmt.Sprintf(tapDevicePathFormat, instanceID)
			break // Found and processed the target adapter
		}
		subkey.Close() // Close if not the target
	} // End loop through subkeys

	if !foundInRegistry {
		return "", "", 0, nil, errors.New("no TAP adapter (ComponentId=" + tapWindowsComponentID + ") found in registry")
	}
	if devPath == "" || instanceID == "" || ifIdx == 0 {
		return "", "", 0, nil, errors.New("internal error: found TAP in registry but failed to retrieve path, GUID, or valid IfIndex via API")
	}

	return devPath, instanceID, ifIdx, currentMAC, nil
}

// --- Create Function ---

// Create initializes the TAP device on Windows.
func Create(config Config) (*Device, error) {
	if config.DevType != TAP {
		return nil, fmt.Errorf("only TAP mode supported by driver")
	}

	// Find/Configure device: sets MAC in registry, gets IfIndex/MAC via API
	devPath, instanceID, ifIndex, currentMAC, err := findAndConfigureTapDevice(config)
	if err != nil {
		return nil, fmt.Errorf("failed find/configure TAP device: %w", err)
	}
	log.Printf("Using TAP device path: %s (GUID: %s), IfIndex: %d, CurrentMAC: %s", devPath, instanceID, ifIndex, currentMAC.String())

	// Open the device handle using CreateFile
	pathPtr, err := windows.UTF16PtrFromString(devPath)
	if err != nil {
		return nil, fmt.Errorf("failed convert path to UTF16: %w", err)
	}

	handle, err := windows.CreateFile(pathPtr, GENERIC_READ|GENERIC_WRITE, FILE_SHARE_READ|FILE_SHARE_WRITE, nil, OPEN_EXISTING, FILE_ATTRIBUTE_SYSTEM|FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		// Handle specific errors
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return nil, fmt.Errorf("TAP device %s not found. Driver installed/adapter enabled? Registry change effect delay?", devPath)
		}
		errno, ok := err.(syscall.Errno)
		if ok && errno == windows.ERROR_ACCESS_DENIED {
			return nil, fmt.Errorf("access denied opening TAP device %s. Run as Administrator?", devPath)
		}
		return nil, fmt.Errorf("failed open TAP device %s: %w", devPath, err)
	}

	// Set media status to 'connected' via IOCTL
	connectStatus := make([]byte, 4)
	connectStatus[0] = 1
	_, err = deviceIoControl(handle, TAP_IOCTL_SET_MEDIA_STATUS, connectStatus, nil)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("failed set TAP media status connected (IOCTL 0x%X): %w", TAP_IOCTL_SET_MEDIA_STATUS, err)
	}

	// Create os.File wrapper and Device struct
	file := os.NewFile(uintptr(handle), devPath)
	dev := &Device{
		File:    file,
		handle:  handle,
		Name:    instanceID, // GUID
		DevType: config.DevType,
		Config:  config,
		ifIndex: ifIndex,    // Store IfIndex
		macAddr: currentMAC, // Store MAC
	}

	// Log ignored Linux config options
	if config.Persist || config.Owner != -1 || config.Group != -1 {
		fmt.Fprintln(os.Stderr, "Warning: Config options Persist, Owner, Group are ignored on Windows.")
	}

	log.Printf("Successfully created and connected TAP device %s (IfIndex: %d)", devPath, ifIndex)
	// Optional: Compare config.MACAddress with currentMAC here if desired
	if config.MACAddress != "" {
		desiredFormatted, _ := formatMACForRegistry(config.MACAddress)
		if currentMAC != nil && !strings.EqualFold(desiredFormatted, fmt.Sprintf("%02X%02X%02X%02X%02X%02X", currentMAC[0], currentMAC[1], currentMAC[2], currentMAC[3], currentMAC[4], currentMAC[5])) {
			log.Printf("Warning: Current MAC %s differs from desired MAC %s set in config/registry.", currentMAC.String(), config.MACAddress)
		} else if currentMAC != nil {
			log.Printf("Confirmed current MAC %s matches desired MAC %s.", currentMAC.String(), config.MACAddress)
		}
	}
	return dev, nil
}

// --- Device Methods ---

// GetIfIndex returns the stored Windows interface index.
func (d *Device) GetIfIndex() uint32 {
	return d.ifIndex
}

// GetMACAddress returns the stored MAC address. Returns a copy.
func (d *Device) GetMACAddress() net.HardwareAddr {
	if d.macAddr == nil {
		return nil
	}
	macCopy := make(net.HardwareAddr, len(d.macAddr))
	copy(macCopy, d.macAddr)
	return macCopy
}

// GetHandle returns the raw Windows device handle.
func (d *Device) GetHandle() windows.Handle {
	return d.handle
}

// Read reads data from the TAP device.
func (d *Device) Read(b []byte) (int, error) { return d.File.Read(b) }

// Write writes data to the TAP device.
func (d *Device) Write(b []byte) (int, error) { return d.File.Write(b) }

// Close closes the TAP device handle.
func (d *Device) Close() error { return d.File.Close() }

// GetFd returns -1 on Windows.
func (d *Device) GetFd() int { return -1 }

// IsTUN checks if the device type is TUN.
func (d *Device) IsTUN() bool { return d.DevType == TUN }

// IsTAP checks if the device type is TAP.
func (d *Device) IsTAP() bool { return d.DevType == TAP }

// SetReadDeadline sets the read deadline.
func (d *Device) SetReadDeadline(t time.Time) error { return d.File.SetReadDeadline(t) }

// SetWriteDeadline sets the write deadline.
func (d *Device) SetWriteDeadline(t time.Time) error { return d.File.SetWriteDeadline(t) }

// SetDeadline sets both read and write deadlines.
func (d *Device) SetDeadline(t time.Time) error {
	if err := d.SetReadDeadline(t); err != nil {
		return err
	}
	return d.SetWriteDeadline(t)
}
