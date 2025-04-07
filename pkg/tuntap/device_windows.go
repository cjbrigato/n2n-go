//go:build windows

package tuntap

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Constants needed for Windows API calls
const (
	// Access rights
	GENERIC_READ  = 0x80000000
	GENERIC_WRITE = 0x40000000
	FILE_SHARE_READ  = 0x00000001
	FILE_SHARE_WRITE = 0x00000002

	// File creation dispositions
	OPEN_EXISTING = 3

	// File attributes and flags
	FILE_ATTRIBUTE_SYSTEM = 0x4
	FILE_FLAG_OVERLAPPED  = 0x40000000 // Recommended for network devices

	// Registry keys and values
	networkAdaptersRegKey = `SYSTEM\CurrentControlSet\Control\Class\{4d36e972-e325-11ce-bfc1-08002be10318}`
	componentIDRegValue   = "ComponentId"
	netCfgInstanceIDValue = "NetCfgInstanceID"
	tapWindowsComponentID = "tap0901" // Component ID for tap-windows6 driver v9.x

	// Device path format
	tapDevicePathFormat = `\\.\Global\%s.tap`
)

// TAP Driver IOCTL Codes (for tap-windows6 v9.x)
// IMPORTANT: These values MUST be verified against the specific tap-windows6 version being used.
//            They are derived from common usage and tap-windows source code inspection.
const (
	// Macro to generate control codes (from Windows DDK ntddk.h or similar)
	// CTL_CODE(DeviceType, Function, Method, Access)
	// DeviceType: FILE_DEVICE_UNKNOWN (0x22) for TAP
	// Method: METHOD_BUFFERED (0)
	// Access: FILE_ANY_ACCESS (0) or FILE_READ_ACCESS | FILE_WRITE_ACCESS (0x3)
	fileDeviceUnknown = 0x00000022
	methodBuffered    = 0
	fileAnyAccess     = 0
	fileReadWriteAccess = 0x00000003 // FILE_READ_ACCESS | FILE_WRITE_ACCESS

	// TAP_IOCTL definitions (Function codes)
	tapIOCTLGetMAC            = 1 // Not reliable, prefer net.InterfaceByName
	tapIOCTLGetVersion        = 2
	tapIOCTLGetMTU            = 3
	tapIOCTLGetInfo           = 4 // Debug info
	tapIOCTLConfigTun         = 5 // Set TUN mode (if supported by driver version)
	tapIOCTLSetMediaStatus    = 6 // *** MOST IMPORTANT: Mark device as connected ***
	tapIOCTLConfigDHCPMasq    = 7
	tapIOCTLGetLogLine        = 8
	tapIOCTLConfigPointToPoint= 10 // Set point-to-point mode

	// Calculate the actual IOCTL codes
	TAP_IOCTL_SET_MEDIA_STATUS = (fileDeviceUnknown << 16) | (fileAnyAccess << 14) | (tapIOCTLSetMediaStatus << 2) | methodBuffered
	// Example: 0x22C018 = (0x22 << 16) | (0 << 14) | (6 << 2) | 0

	// Other codes (calculate similarly if needed)
	// TAP_IOCTL_GET_MAC = (fileDeviceUnknown << 16) | (fileAnyAccess << 14) | (tapIOCTLGetMAC << 2) | methodBuffered // 0x22C004
)

// --- Struct definitions remain the same as in device_linux.go ---
type DeviceType int

const (
	TUN DeviceType = iota // Note: tap-windows6 primarily provides TAP. TUN might need specific IOCTL.
	TAP
)

type Config struct {
	Name        string      // Preferred name (used for lookup, actual may differ)
	DevType     DeviceType  // TUN or TAP
	Persist     bool        // Ignored on Windows
	Owner       int         // Ignored on Windows
	Group       int         // Ignored on Windows
	Permissions os.FileMode // Ignored on Windows
}

type Device struct {
	File    *os.File
	handle  windows.Handle // Store the raw handle for IOCTL calls
	Name    string         // Actual interface name/GUID
	DevType DeviceType
	Config  Config
}

// Helper function to find the TAP adapter device path using the registry
func findTapDevicePath(preferredName string) (string, string, error) {
	// preferredName might be used later for selecting among multiple TAP devices if needed

	key, err := registry.OpenKey(registry.LOCAL_MACHINE, networkAdaptersRegKey, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return "", "", fmt.Errorf("failed to open network adapters registry key: %w", err)
	}
	defer key.Close()

	subkeys, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return "", "", fmt.Errorf("failed to read adapter subkeys: %w", err)
	}

	for _, subkeyName := range subkeys {
		subkey, err := registry.OpenKey(key, subkeyName, registry.QUERY_VALUE)
		if err != nil {
			// Ignore errors for individual subkeys (e.g., permissions)
			continue
		}

		compID, _, err := subkey.GetStringValue(componentIDRegValue)
		if err == nil && compID == tapWindowsComponentID {
			// Found a tap-windows6 adapter! Get its NetCfgInstanceID (GUID)
			instanceID, _, err := subkey.GetStringValue(netCfgInstanceIDValue)
			if err != nil {
				subkey.Close()
				return "", "", fmt.Errorf("found TAP adapter '%s' but failed to get %s: %w", subkeyName, netCfgInstanceIDValue, err)
			}
			subkey.Close()

			// Construct the device path
			devPath := fmt.Sprintf(tapDevicePathFormat, instanceID)
			// The 'actualName' can be considered the instanceID (GUID) for uniqueness
			return devPath, instanceID, nil
		}
		subkey.Close()
	}

	return "", "", errors.New("no tap-windows6 adapter (ComponentId=" + tapWindowsComponentID + ") found in registry")
}

// deviceIoControl sends an IOCTL to the TAP device
func deviceIoControl(handle windows.Handle, ioctl uint32, inBuffer []byte, outBuffer []byte) (uint32, error) {
	var bytesReturned uint32
	var inPtr, outPtr unsafe.Pointer
	var inSize, outSize uint32

	if len(inBuffer) > 0 {
		inPtr = unsafe.Pointer(&inBuffer[0])
		inSize = uint32(len(inBuffer))
	}
	if len(outBuffer) > 0 {
		outPtr = unsafe.Pointer(&outBuffer[0])
		outSize = uint32(len(outBuffer))
	}

	err := windows.DeviceIoControl(handle, ioctl, inPtr, inSize, outPtr, outSize, &bytesReturned, nil)
	if err != nil {
		return bytesReturned, fmt.Errorf("DeviceIoControl failed with code 0x%X: %w", ioctl, err)
	}
	return bytesReturned, nil
}

// Windows implementation of Create
func Create(config Config) (*Device, error) {
	if config.DevType != TAP {
		// tap-windows6 is primarily TAP. Supporting TUN might require
		// specific IOCTLs (like TAP_IOCTL_CONFIG_TUN) if the driver version supports it.
		return nil, fmt.Errorf("only TAP mode is reliably supported by tap-windows6 driver")
	}

	devPath, instanceID, err := findTapDevicePath(config.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to find TAP device: %w.\nEnsure tap-windows6 driver (v9.x with ComponentId %s) is installed", err, tapWindowsComponentID)
	}

	pathPtr, err := windows.UTF16PtrFromString(devPath)
	if err != nil {
		return nil, fmt.Errorf("failed to convert device path to UTF16: %w", err)
	}

	// Open the device
	handle, err := windows.CreateFile(
		pathPtr,
		GENERIC_READ|GENERIC_WRITE,
		FILE_SHARE_READ|FILE_SHARE_WRITE, // Allow other processes to read/write? Maybe restrict?
		nil,                             // Security attributes
		OPEN_EXISTING,
		FILE_ATTRIBUTE_SYSTEM|FILE_FLAG_OVERLAPPED, // Use OVERLAPPED for potential async I/O
		0)                                          // Template file handle

	if err != nil {
		if err == windows.ERROR_FILE_NOT_FOUND {
				return nil, fmt.Errorf("TAP device %s not found. Is the driver installed and the adapter enabled?", devPath)
		}
		// Provide more context on access denied errors
        if errno, ok := err.(syscall.Errno); ok && errno == windows.ERROR_ACCESS_DENIED {
             return nil, fmt.Errorf("access denied opening TAP device %s. Ensure the application is run with Administrator privileges", devPath)
        }
		return nil, fmt.Errorf("failed to open TAP device %s: %w", devPath, err)
	}

	// *** CRITICAL STEP: Set media status to 'connected' ***
	// The input buffer should contain a 32-bit integer (1 for connected)
	connectStatus := make([]byte, 4)
	connectStatus[0] = 1 // 1 = connected, 0 = disconnected
	_, err = deviceIoControl(handle, TAP_IOCTL_SET_MEDIA_STATUS, connectStatus, nil)
	if err != nil {
		windows.CloseHandle(handle)
		return nil, fmt.Errorf("failed to set TAP media status to connected (IOCTL 0x%X): %w. Ensure driver version matches IOCTL codes", TAP_IOCTL_SET_MEDIA_STATUS, err)
	}

	// Wrap the handle in an os.File for easier Read/Write
	// The name used here is just for informational purposes in Go
	file := os.NewFile(uintptr(handle), devPath)

	dev := &Device{
		File:    file,
		handle:  handle, // Keep the raw handle too
		Name:    instanceID, // Use the unique instance ID (GUID) as the effective name
		DevType: config.DevType,
		Config:  config,
	}

	// Ignore Linux-specific config options on Windows
	if config.Persist || config.Owner != -1 || config.Group != -1 {
		fmt.Fprintln(os.Stderr, "Warning: Config options Persist, Owner, Group are ignored on Windows.")
	}


	return dev, nil
}

// --- Methods generally delegate to os.File ---

func (d *Device) Read(b []byte) (int, error) {
	// Note: Using os.File assumes blocking I/O or relies on Go's netpoller integration.
	// For high-performance or truly async I/O, manual OVERLAPPED management with
	// ReadFile/WriteFile and GetOverlappedResult might be needed.
	return d.File.Read(b)
}

func (d *Device) Write(b []byte) (int, error) {
	return d.File.Write(b)
}

func (d *Device) Close() error {
	// Closing the os.File should close the underlying handle.
	return d.File.Close()
	// If we didn't use os.File, we'd call windows.CloseHandle(d.handle)
}

// GetFd returns -1 on Windows as file descriptors are a Unix concept.
func (d *Device) GetFd() int {
	return -1
}

// GetHandle returns the raw Windows HANDLE.
func (d *Device) GetHandle() windows.Handle {
	return d.handle
}

func (d *Device) IsTUN() bool {
	return d.DevType == TUN
}

func (d *Device) IsTAP() bool {
	return d.DevType == TAP
}

// Deadline functions delegate to os.File. Their effectiveness might depend
// on how Go's runtime integrates with OVERLAPPED I/O on the TAP handle.
func (d *Device) SetReadDeadline(t time.Time) error {
	return d.File.SetReadDeadline(t)
}

func (d *Device) SetWriteDeadline(t time.Time) error {
	return d.File.SetWriteDeadline(t)
}

func (d *Device) SetDeadline(t time.Time) error {
	if err := d.SetReadDeadline(t); err != nil {
		return err
	}
	return d.SetWriteDeadline(t)
}