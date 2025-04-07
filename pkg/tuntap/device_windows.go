//go:build windows

package tuntap

import (
	"errors"
	"fmt"
	"net" // For parsing/formatting MAC
	"os"
	"strings" // For MAC formatting
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"n2n-go/pkg/log" // Assuming logging package
)

// --- Constants remain the same ---
const (
	GENERIC_READ          = 0x80000000
	GENERIC_WRITE         = 0x40000000
	FILE_SHARE_READ       = 0x00000001
	FILE_SHARE_WRITE      = 0x00000002
	OPEN_EXISTING         = 3
	FILE_ATTRIBUTE_SYSTEM = 0x4
	FILE_FLAG_OVERLAPPED  = 0x40000000
	networkAdaptersRegKey = `SYSTEM\CurrentControlSet\Control\Class\{4d36e972-e325-11ce-bfc1-08002be10318}`
	componentIDRegValue   = "ComponentId"
	netCfgInstanceIDValue = "NetCfgInstanceID"
	networkAddressValue   = "NetworkAddress" // Standard value name for MAC override
	tapWindowsComponentID = `root\tap0901`
	tapDevicePathFormat   = `\\.\Global\%s.tap`
)

// --- IOCTL Codes remain the same ---
const (
	// ... (keep existing IOCTL definitions) ...
	fileDeviceUnknown          = 0x00000022
	methodBuffered             = 0
	fileAnyAccess              = 0
	tapIOCTLSetMediaStatus     = 6
	TAP_IOCTL_SET_MEDIA_STATUS = (fileDeviceUnknown << 16) | (fileAnyAccess << 14) | (tapIOCTLSetMediaStatus << 2) | methodBuffered // 0x22C018
)

// --- Struct definitions remain the same ---
type DeviceType int

const (
	TUN DeviceType = iota
	TAP
)

// Config struct - ensure MACAddress field is present here too
type Config struct {
	Name        string
	DevType     DeviceType
	MACAddress  string // Desired MAC Address
	Persist     bool
	Owner       int
	Group       int
	Permissions os.FileMode
}

type Device struct {
	File    *os.File
	handle  windows.Handle
	Name    string
	DevType DeviceType
	Config  Config
}

// --- deviceIoControl remains the same (with the *byte fix) ---
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

// formatMACForRegistry converts a standard MAC string (e.g., "00:11:...")
// to the format needed for the NetworkAddress registry value ("0011...").
func formatMACForRegistry(macStr string) (string, error) {
	hwAddr, err := net.ParseMAC(macStr)
	if err != nil {
		// Maybe it's already in the correct format? Check length and hex digits.
		if len(macStr) == 12 {
			isHex := true
			for _, r := range macStr {
				if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
					isHex = false
					break
				}
			}
			if isHex {
				return strings.ToUpper(macStr), nil // Return as is, but uppercase
			}
		}
		return "", fmt.Errorf("invalid MAC address format '%s': %w", macStr, err)
	}
	// Format parsed MAC as uppercase hex string without separators
	return fmt.Sprintf("%02X%02X%02X%02X%02X%02X",
		hwAddr[0], hwAddr[1], hwAddr[2], hwAddr[3], hwAddr[4], hwAddr[5]), nil
}

// findAndConfigureTapDevice searches for the TAP adapter, sets its MAC via registry if needed,
// and returns the device path and instance ID (GUID).
// REQUIRES Administrator privileges if setting MAC address.
func findAndConfigureTapDevice(config Config) (devPath string, instanceID string, err error) {
	targetMAC := ""
	if config.MACAddress != "" {
		targetMAC, err = formatMACForRegistry(config.MACAddress)
		if err != nil {
			return "", "", fmt.Errorf("invalid target MAC address: %w", err)
		}
		log.Printf("Attempting to set MAC address %s via registry", targetMAC)
	}

	key, err := registry.OpenKey(registry.LOCAL_MACHINE, networkAdaptersRegKey, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return "", "", fmt.Errorf("failed to open network adapters registry key: %w. Ensure running as Administrator?", err)
	}
	defer key.Close()

	subkeys, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return "", "", fmt.Errorf("failed to read adapter subkeys: %w", err)
	}

	found := false
	for _, subkeyName := range subkeys {
		// Open with READ and WRITE access if we need to set MAC
		access := uint32(registry.QUERY_VALUE)
		if targetMAC != "" {
			access |= registry.SET_VALUE
		}

		subkey, err := registry.OpenKey(key, subkeyName, access)
		if err != nil {
			// Try read-only if write failed (maybe already configured?)
			subkey, err = registry.OpenKey(key, subkeyName, registry.QUERY_VALUE)
			if err != nil {
				log.Printf("Debug: Skipping registry subkey %s due to open error: %v", subkeyName, err)
				continue // Ignore errors for individual subkeys
			}
			// If we are here, we opened read-only. We can check if it's the TAP adapter,
			// but we cannot set the MAC if targetMAC is specified.
			if targetMAC != "" {
				log.Printf("Warning: Opened TAP adapter registry key %s read-only, cannot set MAC address %s. Check permissions.", subkeyName, targetMAC)
			}
		}

		compID, _, err := subkey.GetStringValue(componentIDRegValue)
		if err == nil && compID == tapWindowsComponentID {
			// Found a tap-windows6 adapter. Get its GUID.
			guid, _, errGuid := subkey.GetStringValue(netCfgInstanceIDValue)
			if errGuid != nil {
				subkey.Close()
				log.Printf("Warning: Found TAP adapter '%s' but failed to get %s: %v", subkeyName, netCfgInstanceIDValue, errGuid)
				continue // Try next subkey
			}

			instanceID = guid // Store the GUID
			found = true      // Mark as found

			// --- Set MAC address in registry if requested ---
			if targetMAC != "" && (access&registry.SET_VALUE != 0) { // Check if we have write access
				log.Printf("Writing NetworkAddress=%s to registry key %s for GUID %s", targetMAC, subkeyName, instanceID)
				errSet := subkey.SetStringValue(networkAddressValue, targetMAC)
				if errSet != nil {
					subkey.Close()
					// Failure to set MAC is critical if requested
					return "", "", fmt.Errorf("failed to set %s=%s in registry for adapter %s (GUID %s): %w. Check Administrator privileges",
						networkAddressValue, targetMAC, subkeyName, instanceID, errSet)
				}
				log.Printf("Successfully wrote NetworkAddress=%s to registry for %s", targetMAC, subkeyName)
			} else if targetMAC != "" {
				// Log if we intended to write but couldn't open with write access
				log.Printf("Warning: Cannot write NetworkAddress to registry key %s (opened read-only). MAC might not be set.", subkeyName)
			}
			// --- End MAC setting ---

			subkey.Close() // Close the current subkey

			// Construct the device path using the found GUID
			devPath = fmt.Sprintf(tapDevicePathFormat, instanceID)

			// Assuming we only configure the *first* TAP adapter found.
			// If multiple TAP adapters exist, more complex logic might be needed
			// to match based on config.Name or other criteria.
			break // Stop searching once found and configured
		}
		subkey.Close() // Close subkey if it wasn't the target TAP adapter
	}

	if !found {
		return "", "", errors.New("no tap-windows6 adapter (ComponentId=" + tapWindowsComponentID + ") found in registry")
	}

	if devPath == "" || instanceID == "" {
		// Should not happen if found=true, but defensive check
		return "", "", errors.New("internal error: found TAP adapter but failed to retrieve path or GUID")
	}

	return devPath, instanceID, nil
}

// Windows implementation of Create
func Create(config Config) (*Device, error) {
	if config.DevType != TAP {
		return nil, fmt.Errorf("only TAP mode is reliably supported by tap-windows6 driver")
	}

	// Find adapter, *set MAC in registry* if specified in config
	devPath, instanceID, err := findAndConfigureTapDevice(config)
	if err != nil {
		// Error could be finding, formatting MAC, or setting registry value
		return nil, fmt.Errorf("failed to find/configure TAP device: %w", err)
	}
	log.Printf("Using TAP device path: %s (GUID: %s)", devPath, instanceID)

	// --- Proceed with opening the device (CreateFile) ---
	pathPtr, err := windows.UTF16PtrFromString(devPath)
	if err != nil {
		return nil, fmt.Errorf("failed to convert device path to UTF16: %w", err)
	}

	handle, err := windows.CreateFile(
		pathPtr,
		GENERIC_READ|GENERIC_WRITE,
		FILE_SHARE_READ|FILE_SHARE_WRITE,
		nil,
		OPEN_EXISTING,
		FILE_ATTRIBUTE_SYSTEM|FILE_FLAG_OVERLAPPED,
		0)

	if err != nil {
		// More specific error checking from before
		if err == windows.ERROR_FILE_NOT_FOUND {
			return nil, fmt.Errorf("TAP device %s not found. Is driver installed/adapter enabled? Did registry changes require adapter restart?", devPath)
		}
		if errno, ok := err.(syscall.Errno); ok && errno == windows.ERROR_ACCESS_DENIED {
			return nil, fmt.Errorf("access denied opening TAP device %s. Ensure running as Administrator", devPath)
		}
		return nil, fmt.Errorf("failed to open TAP device %s after registry config: %w", devPath, err)
	}
	// --- End CreateFile ---

	// --- Set media status (remains crucial) ---
	connectStatus := make([]byte, 4)
	connectStatus[0] = 1 // 1 = connected
	_, err = deviceIoControl(handle, TAP_IOCTL_SET_MEDIA_STATUS, connectStatus, nil)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("failed to set TAP media status to connected (IOCTL 0x%X): %w", TAP_IOCTL_SET_MEDIA_STATUS, err)
	}
	// --- End Set Media Status ---

	file := os.NewFile(uintptr(handle), devPath)
	dev := &Device{
		File:    file,
		handle:  handle,
		Name:    instanceID, // Use GUID as the unique name/ID
		DevType: config.DevType,
		Config:  config, // Store original config
	}

	if config.Persist || config.Owner != -1 || config.Group != -1 {
		fmt.Fprintln(os.Stderr, "Warning: Config options Persist, Owner, Group are ignored on Windows.")
	}

	log.Printf("Successfully created and connected TAP device %s (GUID: %s)", devPath, instanceID)
	// Note: Verify MAC using ipconfig /all manually after creation to confirm registry setting worked.
	return dev, nil
}

// --- Other methods (Read, Write, Close, GetFd, GetHandle, IsTUN/TAP, Deadlines) remain unchanged ---
// ... (keep the rest of the methods as they were) ...

func (d *Device) Read(b []byte) (int, error)         { return d.File.Read(b) }
func (d *Device) Write(b []byte) (int, error)        { return d.File.Write(b) }
func (d *Device) Close() error                       { return d.File.Close() }
func (d *Device) GetFd() int                         { return -1 }
func (d *Device) GetHandle() windows.Handle          { return d.handle }
func (d *Device) IsTUN() bool                        { return d.DevType == TUN }
func (d *Device) IsTAP() bool                        { return d.DevType == TAP }
func (d *Device) SetReadDeadline(t time.Time) error  { return d.File.SetReadDeadline(t) }
func (d *Device) SetWriteDeadline(t time.Time) error { return d.File.SetWriteDeadline(t) }
func (d *Device) SetDeadline(t time.Time) error {
	if err := d.SetReadDeadline(t); err != nil {
		return err
	}
	return d.SetWriteDeadline(t)
}
