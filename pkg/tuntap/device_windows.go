//go:build windows

package tuntap

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall" // Keep for syscall.Errno check if needed
	"unsafe"

	"golang.org/x/sys/windows" // Import the windows package
	"golang.org/x/sys/windows/registry"

	"n2n-go/pkg/log" // Assuming logging package
)

// --- Windows Constants ---
// Use constants directly from the windows package where available.
// Local constants only for things not in the package (like IOCTLs or specific formats).
const (
	// Registry keys/values/ComponentId
	networkAdaptersRegKey = `SYSTEM\CurrentControlSet\Control\Class\{4d36e972-e325-11ce-bfc1-08002be10318}`
	componentIDRegValue   = "ComponentId"
	netCfgInstanceIDValue = "NetCfgInstanceID"
	networkAddressValue   = "NetworkAddress"
	tapWindowsComponentID = "TAP0901" // User specified exact ComponentId
	tapDevicePathFormat   = `\\.\Global\%s.tap`

	// Address families (also in windows pkg, aliasing here is optional)
	// AF_UNSPEC = windows.AF_UNSPEC // Use windows.AF_UNSPEC directly
	// AF_INET   = windows.AF_INET   // Use windows.AF_INET directly
	// AF_INET6  = windows.AF_INET6  // Use windows.AF_INET6 directly

	// Flags for GetAdaptersAddresses
	// GAA_FLAG_INCLUDE_PREFIX = 0x0010 // Use windows.GAA_FLAG_INCLUDE_PREFIX if available, otherwise define locally

)

// --- IOCTL Codes (Defined locally as they are driver-specific) ---
const (
	fileDeviceUnknown          = 0x00000022
	methodBuffered             = 0
	fileAnyAccess              = 0
	tapIOCTLSetMediaStatus     = 6
	TAP_IOCTL_SET_MEDIA_STATUS = (fileDeviceUnknown << 16) | (fileAnyAccess << 14) | (tapIOCTLSetMediaStatus << 2) | methodBuffered
)

// NOTE: DeviceType, Config, Device structs are now defined in types.go

// --- Helper Functions ---

// deviceIoControl (unchanged)
func deviceIoControl(dev *Device, ioctl uint32, inBuffer []byte, outBuffer []byte) (uint32, error) {
	if dev == nil {
		return 0, errors.New("deviceIoControl: device is nil")
	}
	handle := dev.GetHandle()
	if handle == windows.InvalidHandle {
		return 0, errors.New("deviceIoControl: invalid handle")
	}
	var bytesReturned uint32
	var inPtr, outPtr *byte
	var inSize, outSize uint32
	if len(inBuffer) > 0 {
		inPtr = &inBuffer[0]
		inSize = uint32(len(inBuffer))
	}
	if len(outBuffer) > 0 {
		outPtr = &outBuffer[0]
		outSize = uint32(len(outBuffer))
	}
	err := windows.DeviceIoControl(handle, ioctl, inPtr, inSize, outPtr, outSize, &bytesReturned, nil)
	if err != nil {
		return bytesReturned, fmt.Errorf("DeviceIoControl failed IOCTL(0x%X): %w", ioctl, err)
	}
	return bytesReturned, nil
}

// formatMACForRegistry (unchanged)
func formatMACForRegistry(macStr string) (string, error) {
	hwAddr, err := net.ParseMAC(macStr)
	if err != nil {
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
			}
		}
		return "", fmt.Errorf("invalid MAC format '%s': %w", macStr, err)
	}
	return fmt.Sprintf("%02X-%02X-%02X-%02X-%02X-%02X", hwAddr[0], hwAddr[1], hwAddr[2], hwAddr[3], hwAddr[4], hwAddr[5]), nil
}

// findInterfaceIndexAndInfoByGUID (unchanged)
func findInterfaceIndexAndInfoByGUID(guid string) (ifIndex uint32, macAddr net.HardwareAddr, err error) {
	var bufferSize uint32 = 15000
	var adapters *windows.IpAdapterAddresses
	var ret error
	for attempt := 0; attempt < 3; attempt++ {
		buffer := make([]byte, bufferSize)
		if len(buffer) == 0 {
			return 0, nil, errors.New("buffer size zero")
		}
		adapters = (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
		// Use windows.GAA_FLAG_INCLUDE_PREFIX if available, else define locally
		const GAA_FLAG_INCLUDE_PREFIX = 0x0010
		ret = windows.GetAdaptersAddresses(windows.AF_UNSPEC, GAA_FLAG_INCLUDE_PREFIX, 0, adapters, &bufferSize)
		if ret == nil {
			break
		}
		if errors.Is(ret, windows.ERROR_BUFFER_OVERFLOW) {
			log.Printf("Debug: GetAdaptersAddresses needs larger buffer (%d bytes), retrying...", bufferSize)
			continue
		}
		return 0, nil, fmt.Errorf("GetAdaptersAddresses call failed: %w", ret)
	}
	if ret != nil {
		return 0, nil, fmt.Errorf("GetAdaptersAddresses failed after attempts: %w", ret)
	}
	if adapters == nil {
		return 0, nil, errors.New("failed get adapters structure despite API success")
	}
	found := false
	for current := adapters; current != nil; current = current.Next {
		var nameBytes []byte
		namePtr := current.AdapterName
		if namePtr != nil {
			const maxLen = 256
			for i := 0; i < maxLen; i++ {
				byteVal := *(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(namePtr)) + uintptr(i)))
				if byteVal == 0 {
					break
				}
				nameBytes = append(nameBytes, byteVal)
			}
		}
		adapterName := string(nameBytes)
		if adapterName == guid { // Case-sensitive GUID compare
			ifIndex = current.IfIndex
			found = true
			if current.PhysicalAddressLength > 0 && len(current.PhysicalAddress) >= int(current.PhysicalAddressLength) {
				macAddr = make(net.HardwareAddr, current.PhysicalAddressLength)
				copy(macAddr, current.PhysicalAddress[:current.PhysicalAddressLength])
				if len(macAddr) > 6 {
					macAddr = macAddr[:6]
				}
			}
			break
		}
	}
	if !found {
		return 0, nil, fmt.Errorf("adapter GUID %s not found via GetAdaptersAddresses", guid)
	}
	if ifIndex == 0 {
		return 0, nil, fmt.Errorf("found GUID %s but IfIndex is 0", guid)
	}
	return ifIndex, macAddr, nil
}

// findAndConfigureTapDevice (unchanged logic, check prefixes implicitly applied below)
func findAndConfigureTapDevice(config Config) (devPath, instanceID string, ifIdx uint32, currentMAC net.HardwareAddr, err error) {
	targetMAC := ""
	if config.MACAddress != "" {
		targetMAC, err = formatMACForRegistry(config.MACAddress)
		if err != nil {
			return "", "", 0, nil, fmt.Errorf("invalid target MAC: %w", err)
		}
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, networkAdaptersRegKey, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("failed open adapters key: %w", err)
	}
	defer key.Close()
	subkeys, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("failed read subkeys: %w", err)
	}
	foundInRegistry := false
	var foundSubkeyName string
	for _, subkeyName := range subkeys {
		access := uint32(registry.QUERY_VALUE)
		if targetMAC != "" {
			access |= registry.SET_VALUE
		}
		subkey, errOpen := registry.OpenKey(key, subkeyName, access)
		if errOpen != nil && targetMAC != "" {
			subkey, errOpen = registry.OpenKey(key, subkeyName, registry.QUERY_VALUE)
			if errOpen != nil {
				if !errors.Is(errOpen, windows.ERROR_ACCESS_DENIED) {
					log.Printf("Debug: Skipping key %s: %v", subkeyName, errOpen)
				}
				continue
			}
			log.Printf("Warning: Opened TAP key %s read-only, cannot set MAC %s.", subkeyName, targetMAC)
		} else if errOpen != nil {
			if !errors.Is(errOpen, windows.ERROR_ACCESS_DENIED) {
				log.Printf("Debug: Skipping key %s: %v", subkeyName, errOpen)
			}
			continue
		}
		compID, _, errComp := subkey.GetStringValue(componentIDRegValue)
		if errComp == nil && compID == tapWindowsComponentID {
			guid, _, errGuid := subkey.GetStringValue(netCfgInstanceIDValue)
			if errGuid != nil {
				log.Printf("Warning: Found TAP key '%s' failed get GUID: %v", subkeyName, errGuid)
				subkey.Close()
				continue
			}
			instanceID = guid
			foundInRegistry = true
			foundSubkeyName = subkeyName
			subkey.Close()
			break
		}
		subkey.Close()
	}
	if !foundInRegistry {
		return "", "", 0, nil, errors.New("no TAP adapter (ComponentId=" + tapWindowsComponentID + ") found in registry")
	}
	ifIdx, currentMAC, err = findInterfaceIndexAndInfoByGUID(instanceID)
	if err != nil {
		return "", "", 0, nil, fmt.Errorf("found TAP in registry (GUID %s) but API lookup failed: %w", instanceID, err)
	}
	if targetMAC != "" {
		subkeyWrite, errOpenWrite := registry.OpenKey(key, foundSubkeyName, registry.SET_VALUE)
		if errOpenWrite != nil {
			return "", "", 0, nil, fmt.Errorf("failed open key %s for write access: %w. Admin?", foundSubkeyName, errOpenWrite)
		}
		defer subkeyWrite.Close()
		errSet := subkeyWrite.SetStringValue(networkAddressValue, targetMAC)
		if errSet != nil {
			return "", "", 0, nil, fmt.Errorf("failed write NetworkAddress=%s to key %s: %w", targetMAC, foundSubkeyName, errSet)
		}
		log.Printf("Successfully wrote NetworkAddress=%s to registry key %s.", targetMAC, foundSubkeyName)
	}
	devPath = fmt.Sprintf(tapDevicePathFormat, instanceID)
	if devPath == "" || instanceID == "" || ifIdx == 0 {
		return "", "", 0, nil, errors.New("internal error finalizing path/GUID/IfIndex")
	}
	return devPath, instanceID, ifIdx, currentMAC, nil
}

/*
// findAndConfigureTapDevice searches registry, validates MAC, sets MAC, finds IfIndex/MAC via API.
func findAndConfigureTapDevice(config Config) (devPath, instanceID string, ifIdx uint32, currentMAC net.HardwareAddr, err error) {
	var targetMAC net.HardwareAddr // Store the parsed MAC
	var targetMACRegistryFormat string // Store the formatted string for registry

	if config.MACAddress != "" {
		// 1. Parse the provided MAC string
		targetMAC, err = net.ParseMAC(config.MACAddress)
		if err != nil {
			return "", "", 0, nil, fmt.Errorf("invalid MAC address format in config '%s': %w", config.MACAddress, err)
		}
        if len(targetMAC) != 6 {
             return "", "", 0, nil, fmt.Errorf("parsed MAC address '%s' is not 6 bytes long", targetMAC.String())
        }


		// 2. Validate the parsed MAC address
        // Check for Multicast bit (LSB of first octet)
        if (targetMAC[0] & 0x01) != 0 {
            return "", "", 0, nil, fmt.Errorf("invalid MAC address '%s': multicast bit is set (must not be multicast)", targetMAC.String())
        }
        // Check for Locally Administered bit (second LSB of first octet)
        if (targetMAC[0] & 0x02) == 0 {
            // This is often a requirement or strong recommendation for virtual adapters
            log.Printf("Warning: MAC address '%s' does not have the locally administered bit set. This might cause issues.", targetMAC.String())
            // Depending on strictness, you might choose to return an error here instead:
            // return "", "", 0, nil, fmt.Errorf("invalid MAC address '%s': locally administered bit is not set", targetMAC.String())
        }

		// 3. Format for registry (assuming your version now includes hyphens)
		targetMACRegistryFormat, err = formatMACForRegistry(config.MACAddress) // Use the formatter
		if err != nil {
			// Should not happen if ParseMAC succeeded, but check defensively
			return "", "", 0, nil, fmt.Errorf("failed to format validated MAC '%s' for registry: %w", targetMAC.String(), err)
		}
		log.Printf("Validated and formatted target MAC for registry: %s", targetMACRegistryFormat)
	}

	// --- Registry search logic remains the same ---
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, networkAdaptersRegKey, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil { return "", "", 0, nil, fmt.Errorf("failed open adapters key: %w", err) }; defer key.Close()
	subkeys, err := key.ReadSubKeyNames(-1); if err != nil { return "", "", 0, nil, fmt.Errorf("failed read subkeys: %w", err) }

	foundInRegistry := false; var foundSubkeyName string
	for _, subkeyName := range subkeys {
		access := uint32(registry.QUERY_VALUE); if targetMACRegistryFormat != "" { access |= registry.SET_VALUE } // Check if formatted string exists
		subkey, errOpen := registry.OpenKey(key, subkeyName, access)
		if errOpen != nil && targetMACRegistryFormat != "" { } else if errOpen != nil {  continue }

		compID, _, errComp := subkey.GetStringValue(componentIDRegValue)
		if errComp == nil && compID == tapWindowsComponentID {
			guid, _, errGuid := subkey.GetStringValue(netCfgInstanceIDValue); if errGuid != nil { subkey.Close(); continue }
			instanceID = guid; foundInRegistry = true; foundSubkeyName = subkeyName; subkey.Close(); break
		}
		subkey.Close()
	}
	if !foundInRegistry { return "", "", 0, nil, errors.New("no TAP adapter (ComponentId=" + tapWindowsComponentID + ") found in registry") }

	// --- API lookup remains the same ---
	ifIdx, currentMAC, err = findInterfaceIndexAndInfoByGUID(instanceID); if err != nil { return "", "", 0, nil, fmt.Errorf("found TAP in registry (GUID %s) but API lookup failed: %w", instanceID, err) }

	// --- Set registry value using the validated+formatted string ---
	if targetMACRegistryFormat != "" {
		subkeyWrite, errOpenWrite := registry.OpenKey(key, foundSubkeyName, registry.SET_VALUE); if errOpenWrite != nil { return "", "", 0, nil, fmt.Errorf("failed open key %s for write: %w", foundSubkeyName, errOpenWrite) }; defer subkeyWrite.Close()
		errSet := subkeyWrite.SetStringValue(networkAddressValue, targetMACRegistryFormat) // Use formatted string
		if errSet != nil { return "", "", 0, nil, fmt.Errorf("failed write NetworkAddress=%s: %w", targetMACRegistryFormat, errSet) }
		log.Printf("Successfully wrote NetworkAddress=%s to registry key %s.", targetMACRegistryFormat, foundSubkeyName)
	}

	// --- Return results ---
	devPath = fmt.Sprintf(tapDevicePathFormat, instanceID)
	// ... (final checks and return) ...
    if devPath == "" || instanceID == "" || ifIdx == 0 { return "", "", 0, nil, errors.New("internal error finalizing") }
	return devPath, instanceID, ifIdx, currentMAC, nil
}

// --- Make sure formatMACForRegistry produces XX-XX-XX-XX-XX-XX ---
func formatMACForRegistry(macStr string) (string, error) {
	hwAddr, err := net.ParseMAC(macStr)
	if err != nil {
		// Could add check for already hyphenated format here if desired
		return "", fmt.Errorf("invalid MAC format '%s': %w", macStr, err)
	}
	return fmt.Sprintf("%02X-%02X-%02X-%02X-%02X-%02X", // Ensure hyphens are here
		hwAddr[0], hwAddr[1], hwAddr[2], hwAddr[3], hwAddr[4], hwAddr[5]), nil
}
*/

// --- Windows Create function ---
// Creates the TAP device, configures registry, opens handle, stores state.
func Create(config Config) (*Device, error) {
	if config.DevType != TAP {
		return nil, errors.New("only TAP mode supported")
	}

	devPath, instanceID, ifIndex, initialMAC, err := findAndConfigureTapDevice(config)
	if err != nil {
		return nil, fmt.Errorf("failed find/configure TAP device: %w", err)
	}
	log.Printf("Using TAP device path: %s (GUID: %s), IfIndex: %d, InitialMAC: %s", devPath, instanceID, ifIndex, initialMAC.String())

	pathPtr, err := windows.UTF16PtrFromString(devPath)
	if err != nil {
		return nil, fmt.Errorf("failed convert path: %w", err)
	}

	// --- CORRECTED: Add windows. prefix to CreateFile constants ---
	winHandle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,       // Prefixed
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, // Prefixed
		nil,
		windows.OPEN_EXISTING, // Prefixed
		windows.FILE_ATTRIBUTE_SYSTEM|windows.FILE_FLAG_OVERLAPPED, //|windows.FILE_FLAG_OVERLAPPED, // Prefixed
		0)
	// --- End Correction ---

	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return nil, fmt.Errorf("TAP device %s not found: %w", devPath, err)
		}
		// Using syscall.Errno check might still be necessary if errors.Is doesn't work well across packages/types
		errno, ok := err.(syscall.Errno)
		if ok && errno == windows.ERROR_ACCESS_DENIED {
			return nil, fmt.Errorf("access denied opening TAP %s: %w", devPath, err)
		}
		// Fallback error
		return nil, fmt.Errorf("failed open TAP device %s: %w", devPath, err)
	}

	// Create the core Device struct
	file := os.NewFile(uintptr(winHandle), devPath)
	dev := &Device{
		File:    file,
		handle:  uintptr(winHandle), // Store as uintptr
		Name:    instanceID,
		DevType: config.DevType,
		Config:  config,
		ifIndex: ifIndex,
		macAddr: initialMAC,
	}

	// Set media status using the helper
	connectStatus := make([]byte, 4)
	connectStatus[0] = 1
	_, err = deviceIoControl(dev, TAP_IOCTL_SET_MEDIA_STATUS, connectStatus, nil)
	if err != nil {
		windows.CloseHandle(winHandle) // Close original handle on failure
		dev.File = nil
		dev.handle = 0 // Invalidate struct state
		return nil, fmt.Errorf("failed set TAP media status connected: %w", err)
	}

	// ... (Log ignored options, MAC comparison) ...
	log.Printf("Successfully created TAP device %s (IfIndex: %d)", devPath, ifIndex)
	return dev, nil
}

// --- Platform-specific implementations for Device methods ---

// GetIfIndex returns the stored Windows interface index.
func (d *Device) GetIfIndex() uint32 {
	if d == nil {
		return 0
	}
	return d.ifIndex
}

// GetMACAddress returns the stored MAC address. Returns a copy.
func (d *Device) GetMACAddress() net.HardwareAddr {
	if d == nil || d.macAddr == nil {
		return nil
	}
	macCopy := make(net.HardwareAddr, len(d.macAddr))
	copy(macCopy, d.macAddr)
	return macCopy
}

// GetHandle returns the raw Windows device handle by casting the stored uintptr.
func (d *Device) GetHandle() windows.Handle {
	if d == nil {
		return windows.InvalidHandle
	}
	return windows.Handle(d.handle)
}

// NOTE: Read, Write, Close, GetFd, IsTUN/TAP, Set*Deadline methods are
//       defined on *Device in types.go and delegate to the File field.
