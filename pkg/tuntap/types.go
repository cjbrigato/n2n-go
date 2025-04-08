package tuntap

import (
	"net"
	"os"
	"time"
	// REMOVED: "golang.org/x/sys/windows"
)

// --- Common Type Definitions ---

type DeviceType int

const (
	TUN DeviceType = iota
	TAP
)

type Config struct {
	Name        string
	DevType     DeviceType
	MACAddress  string      // Windows: Set via registry
	Persist     bool        // Linux only
	Owner       int         // Linux only
	Group       int         // Linux only
	Permissions os.FileMode // Linux only
}

type Device struct {
	// Common fields
	File    *os.File
	Name    string // Linux: Name hint/actual; Windows: GUID
	DevType DeviceType
	Config  Config

	// --- Platform-specific state stored internally ---

	// Windows-specific state (populated during Create in device_windows.go)
	handle  uintptr // <<<< CHANGED: Store handle as uintptr
	ifIndex uint32
	macAddr net.HardwareAddr

	// Linux-specific state (none needed currently)
}

// GetHandle returns the raw Windows handle.
// This method is ONLY defined in device_windows.go
// func (d *Device) GetHandle() windows.Handle

// --- Common Methods (Read/Write/Close etc. delegate to File) ---
func (d *Device) Read(b []byte) (int, error) {
	if d.File == nil {
		return 0, os.ErrInvalid
	}
	return d.File.Read(b)
}
func (d *Device) Write(b []byte) (int, error) {
	if d.File == nil {
		return 0, os.ErrInvalid
	}
	return d.File.Write(b)
}
func (d *Device) Close() error {
	if d.File == nil {
		return nil
	}
	err := d.File.Close()
	d.File = nil
	return err
}
func (d *Device) GetFd() int {
	if d.File == nil {
		return -1
	}
	return int(d.File.Fd())
}
func (d *Device) IsTUN() bool { return d.DevType == TUN }
func (d *Device) IsTAP() bool { return d.DevType == TAP }
func (d *Device) SetReadDeadline(t time.Time) error {
	if d.File == nil {
		return os.ErrInvalid
	}
	return d.File.SetReadDeadline(t)
}
func (d *Device) SetWriteDeadline(t time.Time) error {
	if d.File == nil {
		return os.ErrInvalid
	}
	return d.File.SetWriteDeadline(t)
}
func (d *Device) SetDeadline(t time.Time) error {
	if d.File == nil {
		return os.ErrInvalid
	}
	if err := d.SetReadDeadline(t); err != nil {
		return err
	}
	return d.SetWriteDeadline(t)
}
