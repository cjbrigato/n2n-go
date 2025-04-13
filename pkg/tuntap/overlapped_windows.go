//go:build windows

package tuntap

// Original code : git.sr.ht/~errnoh/overlapped

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const (
	// OverlappedBlocking indicates that Read/Write operations should block indefinitely.
	OverlappedBlocking = -1
	// OverlappedNonBlocking indicates that Read/Write operations should return immediately (ERROR_IO_PENDING or success).
	OverlappedNonBlocking = 0
	// Use values > 0 for millisecond timeouts.
)

// Handle provides overlapped I/O capabilities for a Windows handle.
// It manages event objects for asynchronous operations.
type Handle struct {
	h windows.Handle
	m sync.Mutex       // Protects the event pool (e)
	e []windows.Handle // Pool of reusable event handles

	// defaultTimeout allows changing the default timeout used for Write() and Read() functions.
	// Set via New(), defaults to OverlappedBlocking (-1).
	defaultTimeout int
	readDeadline   time.Time
	writeDeadline  time.Time
}

func (f *Handle) Fd() uintptr {
	return uintptr(f.h)
}

// getEvent retrieves or creates a reusable event handle for an overlapped operation.
func (f *Handle) getEvent() (windows.Handle, error) {
	f.m.Lock()
	if len(f.e) == 0 {
		f.m.Unlock()
		// Create a manual-reset event, initially non-signaled.
		// We pass nil for security attributes and name.
		e, err := windows.CreateEvent(nil, 1, 0, nil) // Manual reset = 1, Initial state = 0 (non-signaled)
		if err != nil {
			// Wrap the error for context instead of panicking
			return 0, fmt.Errorf("CreateEvent failed: %w", err)
		}
		// Reset event just to be safe, though CreateEvent with 0 should ensure it.
		if err := windows.ResetEvent(e); err != nil {
			windows.CloseHandle(e) // Clean up if ResetEvent fails
			return 0, fmt.Errorf("ResetEvent failed after create: %w", err)
		}
		return e, nil
	}
	e := f.e[len(f.e)-1]
	f.e = f.e[:len(f.e)-1]
	f.m.Unlock()
	return e, nil
}

// putEvent returns an event handle to the pool after resetting it.
func (f *Handle) putEvent(e windows.Handle) {
	// Ignore ResetEvent error, as recovery is difficult and unlikely needed.
	// Log potential errors if necessary in production: log.Printf("warning: ResetEvent failed: %v", err)
	_ = windows.ResetEvent(e)
	f.m.Lock()
	// TODO: Consider adding a maximum pool size check here if needed.
	f.e = append(f.e, e)
	f.m.Unlock()
}

// asyncIo performs the core overlapped I/O logic.
// fn: The function to call (e.g., windows.ReadFile, windows.WriteFile).
// b: The data buffer.
// milliseconds: Timeout duration (-1=blocking, 0=non-blocking, >0=timeout).
// o: The initialized Overlapped structure (HEvent must be set).
// Returns the number of bytes transferred and an error.
// Now returns os.ErrDeadlineExceeded on timeout for consistency
func (f *Handle) asyncIo(fn func(h windows.Handle, p []byte, n *uint32, o *windows.Overlapped) error, b []byte, milliseconds int, o *windows.Overlapped) (uint32, error) {
	var n uint32
	err := fn(f.h, b, &n, o)

	if errors.Is(err, windows.ERROR_IO_PENDING) {
		var waitTimeout uint32
		// Note: milliseconds == 0 means non-blocking check, > 0 means timeout, < 0 means infinite
		if milliseconds < 0 {
			waitTimeout = windows.INFINITE
		} else {
			// Treat 0ms timeout correctly
			waitTimeout = uint32(milliseconds)
		}

		event, waitErr := windows.WaitForSingleObject(o.HEvent, waitTimeout)
		if waitErr != nil {
			return 0, fmt.Errorf("WaitForSingleObject failed directly: %w", waitErr)
		}

		switch event {
		case windows.WAIT_OBJECT_0:
			// Operation completed.
			break // Fall through to GetOverlappedResult.
		case syscall.WAIT_TIMEOUT: // Compared using syscall constant
			// --- FIX ---
			// Check if the timeout was due to a non-blocking request (milliseconds == 0)
			// or an actual expired timer (milliseconds > 0).
			if milliseconds == 0 {
				// Non-blocking call, operation is still pending.
				// Return the original ERROR_IO_PENDING to indicate this state.
				// The caller (ReadTimeout/WriteTimeout) requested non-blocking,
				// so "pending" is the expected status if it wasn't immediately ready.
				break
				//return 0, windows.ERROR_IO_PENDING
			} else {
				// Actual timeout (milliseconds > 0 expired). Cancel and return deadline error.
				_ = windows.CancelIoEx(f.h, o)
				return 0, os.ErrDeadlineExceeded // Use standard Go deadline error
			}
			// --- END FIX --
		case syscall.WAIT_ABANDONED: // Compared using syscall constant
			// Return the specific windows error type
			return 0, fmt.Errorf("WaitForSingleObject: %w", windows.WAIT_ABANDONED)
		case windows.WAIT_FAILED:
			return 0, fmt.Errorf("WaitForSingleObject returned WAIT_FAILED: %w", RGetLastError())
		default:
			return 0, fmt.Errorf("WaitForSingleObject returned unexpected status %d", event)
		}

		// Retrieve the final result.
		err = windows.GetOverlappedResult(f.h, o, &n, true)
		if err != nil {
			if errors.Is(err, windows.ERROR_HANDLE_EOF) {
				return n, io.EOF
			}
			// Check if the aborted operation resulted in a specific error after cancellation
			if errors.Is(err, windows.ERROR_OPERATION_ABORTED) {
				// Map aborted operation due to timeout/cancel also to DeadlineExceeded
				return 0, os.ErrDeadlineExceeded
			}
			return 0, fmt.Errorf("GetOverlappedResult: %w", err)
		}
		return n, nil // Success after pending

	} else if err != nil {
		// Synchronous failure
		if errors.Is(err, windows.ERROR_HANDLE_EOF) {
			return n, io.EOF
		}
		return 0, fmt.Errorf("synchronous operation failed: %w", err)
	}

	// Synchronous success
	return n, nil
}

// calculateTimeoutMillis determines the effective timeout in milliseconds based on a deadline.
// Returns (timeoutMillis, immediateError).
// If immediateError is not nil, the operation should fail immediately.
// timeoutMillis follows the convention: -1=blocking, 0=non-blocking, >0=timeout.
func (f *Handle) calculateTimeoutMillis(deadline time.Time, defaultTimeout int) (int, error) {
	f.m.Lock()
	d := deadline // Read under lock
	f.m.Unlock()

	if d.IsZero() {
		// No deadline set, use the default timeout provided.
		return defaultTimeout, nil
	}

	timeoutDuration := time.Until(d)
	if timeoutDuration <= 0 {
		// Deadline has already passed.
		return 0, os.ErrDeadlineExceeded // Return 0 timeout conceptually, but error takes precedence
	}

	// Calculate milliseconds, ensure it's at least 0 (treat sub-millisecond as 0).
	// MaxInt32 check prevents overflow when casting large int64 to int for uint32 later.
	const maxInt32 = 1<<31 - 1
	millis := timeoutDuration.Milliseconds()
	if millis <= 0 {
		return 0, nil // Sub-millisecond positive duration, treat as non-blocking check (0ms)
	}
	if millis > maxInt32 { // Very large timeout requested
		return maxInt32, nil // Cap at maximum positive int
	}
	return int(millis), nil
}

// SetReadDeadline sets the deadline for future Read calls and any
// currently-blocked Read call. A zero value for t means Read will not time out.
func (f *Handle) SetReadDeadline(t time.Time) error {
	f.m.Lock()
	f.readDeadline = t
	f.m.Unlock()
	// Note: Unlike net.Conn, this implementation doesn't actively interrupt
	// a *currently blocked* WaitForSingleObject call when the deadline changes.
	// The deadline takes effect on the *next* check within the I/O methods.
	// True interruption would require more complex signaling (e.g., dedicated cancel events).
	return nil
}

// SetWriteDeadline sets the deadline for future Write calls and any
// currently-blocked Write call. A zero value for t means Write will not time out.
func (f *Handle) SetWriteDeadline(t time.Time) error {
	f.m.Lock()
	f.writeDeadline = t
	f.m.Unlock()
	// See note in SetReadDeadline about not interrupting currently blocked calls.
	return nil
}

// RGetLastError wraps windows.GetLastError, returning nil if the error code is 0.
// Useful because GetLastError() can return 0 even if an API indicates failure.
func RGetLastError() error {
	if err := windows.GetLastError(); err != nil {
		// Check for ERROR_SUCCESS (0) explicitly, although GetLastError()
		// returning non-nil should imply it's not 0.
		if errno, ok := err.(syscall.Errno); ok && errno == 0 {
			return nil // Treat ERROR_SUCCESS as no error
		}
		return err
	}
	return nil // No error code set
}

// GetDefaultTimeout returns the default timeout configured for this Handle.
func (f *Handle) GetDefaultTimeout() int {
	return f.defaultTimeout
}

// Read reads using the default timeout, but respects the read deadline if set.
func (f *Handle) Read(b []byte) (int, error) {
	// Pass defaultTimeout, ReadTimeout will override if deadline is set
	return f.ReadTimeout(b, f.defaultTimeout)
}

// Write writes using the default timeout, but respects the write deadline if set.
func (f *Handle) Write(b []byte) (int, error) {
	// Pass defaultTimeout, WriteTimeout will override if deadline is set
	return f.WriteTimeout(b, f.defaultTimeout)
}

// Custom allows I/O using a function that has the same signature as windows.ReadFile/windows.WriteFile.
func (f *Handle) Custom(fn func(windows.Handle, []byte, *uint32, *windows.Overlapped) error, b []byte, milliseconds int) (int, error) {
	o := &windows.Overlapped{}
	e, err := f.getEvent()
	if err != nil {
		return 0, fmt.Errorf("custom: %w", err)
	}
	defer f.putEvent(e)
	o.HEvent = e

	n, err := f.asyncIo(fn, b, milliseconds, o)
	// Wrap the error from asyncIo, preserving io.EOF if it was returned.
	if err != nil && !errors.Is(err, io.EOF) {
		err = fmt.Errorf("custom: %w", err)
	}
	return int(n), err
}

// ReadAt reads from offset, using default timeout, but respects read deadline.
func (f *Handle) ReadAt(b []byte, off int64) (int, error) {
	effectiveMillis, err := f.calculateTimeoutMillis(f.readDeadline, f.defaultTimeout)
	if err != nil { // Deadline already expired
		return 0, err
	}

	o := &windows.Overlapped{}
	o.Offset = uint32(off & 0xFFFFFFFF)
	o.OffsetHigh = uint32(off >> 32)

	e, evErr := f.getEvent()
	if evErr != nil {
		return 0, fmt.Errorf("readAt: %w", evErr)
	}
	defer f.putEvent(e)
	o.HEvent = e

	// Use calculated effectiveMillis
	n, err := f.asyncIo(windows.ReadFile, b, effectiveMillis, o)

	// --- Revised EOF Handling ---
	opErr := err
	isStdEOF := (opErr == nil && n == 0 && len(b) > 0)
	isExplicitEOF := errors.Is(opErr, io.EOF)
	// Also treat DeadlineExceeded as a distinct error, not EOF
	isDeadline := errors.Is(opErr, os.ErrDeadlineExceeded)

	if isStdEOF || (isExplicitEOF && !isDeadline) { // Don't mask DeadlineExceeded with EOF
		return int(n), io.EOF
	} else if opErr != nil {
		// Wrap only if not already EOF or DeadlineExceeded
		if !isExplicitEOF && !isDeadline {
			return int(n), fmt.Errorf("readAt: %w", opErr)
		}
		return int(n), opErr // Return original EOF or DeadlineExceeded
	}
	return int(n), nil // Success
}

// WriteAt writes at offset, using default timeout, but respects write deadline.
func (f *Handle) WriteAt(b []byte, off int64) (int, error) {
	effectiveMillis, err := f.calculateTimeoutMillis(f.writeDeadline, f.defaultTimeout)
	if err != nil { // Deadline already expired
		return 0, err
	}

	o := &windows.Overlapped{}
	o.Offset = uint32(off & 0xFFFFFFFF)
	o.OffsetHigh = uint32(off >> 32)

	e, evErr := f.getEvent()
	if evErr != nil {
		return 0, fmt.Errorf("writeAt: %w", evErr)
	}
	defer f.putEvent(e)
	o.HEvent = e

	// Use calculated effectiveMillis
	n, err := f.asyncIo(windows.WriteFile, b, effectiveMillis, o)
	if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) { // Don't wrap DeadlineExceeded
		return int(n), fmt.Errorf("writeAt: %w", err)
	}
	return int(n), err // Return result from asyncIo (nil, DeadlineExceeded, or other wrapped error)
}

// ReadTimeout reads, allowing custom timeout, but respects read deadline (which takes precedence).
func (f *Handle) ReadTimeout(b []byte, milliseconds int) (int, error) {
	// Calculate effective timeout: deadline overrides explicit milliseconds/default
	effectiveMillis, err := f.calculateTimeoutMillis(f.readDeadline, milliseconds)
	if err != nil { // Deadline already expired
		return 0, err
	}

	o := &windows.Overlapped{}
	e, evErr := f.getEvent()
	if evErr != nil {
		return 0, fmt.Errorf("read: %w", evErr)
	}
	defer f.putEvent(e)
	o.HEvent = e

	// Use calculated effectiveMillis
	n, err := f.asyncIo(windows.ReadFile, b, effectiveMillis, o)

	// --- Revised EOF Handling (same as ReadAt) ---
	opErr := err
	isStdEOF := (opErr == nil && n == 0 && len(b) > 0)
	isExplicitEOF := errors.Is(opErr, io.EOF)
	isDeadline := errors.Is(opErr, os.ErrDeadlineExceeded)

	if isStdEOF || (isExplicitEOF && !isDeadline) {
		return int(n), io.EOF
	} else if opErr != nil {
		if !isExplicitEOF && !isDeadline {
			return int(n), fmt.Errorf("read: %w", opErr)
		}
		return int(n), opErr
	}

	return int(n), nil
}

// WriteTimeout writes, allowing custom timeout, but respects write deadline (which takes precedence).
func (f *Handle) WriteTimeout(b []byte, milliseconds int) (int, error) {
	// Calculate effective timeout: deadline overrides explicit milliseconds/default
	effectiveMillis, err := f.calculateTimeoutMillis(f.writeDeadline, milliseconds)
	if err != nil { // Deadline already expired
		return 0, err
	}

	o := &windows.Overlapped{}
	e, evErr := f.getEvent()
	if evErr != nil {
		return 0, fmt.Errorf("write: %w", evErr)
	}
	defer f.putEvent(e)
	o.HEvent = e

	// Use calculated effectiveMillis
	n, err := f.asyncIo(windows.WriteFile, b, effectiveMillis, o)
	if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) && !errors.Is(err, windows.ERROR_IO_PENDING) {
		return int(n), fmt.Errorf("write: %w", err)
	}
	return int(n), err
}

// Close remains the same
// Close cancels pending I/O, closes the wrapped handle, and releases associated event handles.
// It is safe to call Close multiple times.
func (f *Handle) Close() error {
	// Idempotency check: If handle is already zero or invalid, do nothing.
	// Using 0 as the marker for closed.
	if f.h == 0 || f.h == windows.InvalidHandle {
		return nil // Or return os.ErrClosed or similar if needed
	}

	// Attempt to cancel any pending I/O operations initiated by this handle.
	// Ignore errors, as the handle might already be closing or invalid.
	_ = windows.CancelIoEx(f.h, nil)

	// Close the underlying Windows handle.
	err := windows.CloseHandle(f.h)
	// Mark handle as closed regardless of CloseHandle error.
	f.h = 0

	// Lock the event pool mutex to safely close and clear the pool.
	f.m.Lock()
	for _, h := range f.e {
		_ = windows.CloseHandle(h) // Ignore errors closing event handles
	}
	f.e = nil // Clear the slice
	f.m.Unlock()

	// Return the error from closing the main handle, if any.
	if err != nil {
		return fmt.Errorf("CloseHandle: %w", err)
	}
	return nil
}

// NewOverlapped returns a new overlapped.Handle that wraps the original handle.
// The defaultTimeout specifies the behavior for Read() and Write() (-1=blocking, 0=non-blocking, >0=timeout ms).
func NewOverlapped(h windows.Handle, defaultTimeout int) *Handle {
	// Note: We don't validate the handle 'h' itself here. It's assumed valid.
	// Check if h needs FILE_FLAG_OVERLAPPED? The caller is responsible for opening the handle correctly.
	return &Handle{
		h:              h,
		defaultTimeout: defaultTimeout,
		e:              make([]windows.Handle, 0, 4), // Pre-allocate small capacity for event pool
		readDeadline:   time.Time{},                  // Explicitly zero
		writeDeadline:  time.Time{},                  // Explicitly zero
	}
}

// --- PositionHandle ---

// PositionHandle wraps overlapped.Handle, maintaining an internal position
// for sequential Read/Write operations similar to os.File.
// It uses an internal mutex for safe concurrent access, serializing operations
// on the *same* PositionHandle instance. Supports large files (>4GB).
type PositionHandle struct {
	*Handle             // Embed the base overlapped Handle
	position int64      // Current position (supports large files)
	posMutex sync.Mutex // Protects access to the position field
}

// NewPositionHandle creates a new PositionHandle wrapping the given windows handle.
// The initial position is set to 0.
// The defaultTimeout specifies the behavior for Read() and Write() (-1=blocking, 0=non-blocking, >0=timeout ms).
func NewPositionHandle(h windows.Handle, defaultTimeout int) *PositionHandle {
	return &PositionHandle{
		Handle:   NewOverlapped(h, defaultTimeout), // Create the underlying overlapped Handle
		position: 0,                                // Start at position 0
	}
}

// Read reads forward from the current position and advances the position
// by the number of bytes read. Safe for concurrent use.
func (f *PositionHandle) Read(b []byte) (int, error) {
	f.posMutex.Lock()        // Lock before accessing/modifying position
	currentPos := f.position // Read position safely

	o := &windows.Overlapped{}
	o.Offset = uint32(currentPos & 0xFFFFFFFF)
	o.OffsetHigh = uint32(currentPos >> 32)

	// Unlock happens deferred *after* position is potentially updated.
	defer f.posMutex.Unlock()

	// getEvent/putEvent are handled within Handle methods, which are thread-safe.
	// Call the embedded Handle's ReadAt method, which handles asyncIo, events, and large offsets.
	// We use ReadAt here because PositionHandle fundamentally works with offsets.
	// ReadAt already contains the revised EOF logic.
	n, err := f.Handle.ReadAt(b, currentPos) // Use ReadAt with the captured position

	// Update position based on bytes actually read (n).
	// This must happen *before* unlocking.
	if n > 0 {
		f.position = currentPos + int64(n)
	}

	// Return the result from ReadAt directly. It already wraps errors appropriately.
	return n, err
}

// Write writes to the current position and advances the position
// by the number of bytes written. Safe for concurrent use.
func (f *PositionHandle) Write(b []byte) (int, error) {
	f.posMutex.Lock()        // Lock before accessing/modifying position
	currentPos := f.position // Read position safely

	o := &windows.Overlapped{}
	o.Offset = uint32(currentPos & 0xFFFFFFFF)
	o.OffsetHigh = uint32(currentPos >> 32)

	// Unlock happens deferred *after* position is potentially updated.
	defer f.posMutex.Unlock()

	// Call the embedded Handle's WriteAt method.
	n, err := f.Handle.WriteAt(b, currentPos) // Use WriteAt with the captured position

	// Update position based on bytes actually written (n).
	// This must happen *before* unlocking.
	if n > 0 {
		f.position = currentPos + int64(n)
	}

	// Return the result from WriteAt directly. It already wraps errors appropriately.
	return n, err
}

// Close closes the underlying Handle. It's important to call this
// to release the file handle and event handles.
func (f *PositionHandle) Close() error {
	// Delegate closing to the embedded Handle's Close method.
	return f.Handle.Close()
}

// Note: PositionHandle still does not implement io.Seeker.
// Implementing Seek would require acquiring f.posMutex and updating f.position.
// Example Seek implementation (basic):
/*
func (f *PositionHandle) Seek(offset int64, whence int) (int64, error) {
	f.posMutex.Lock()
	defer f.posMutex.Unlock()

	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = f.position + offset
	case io.SeekEnd:
		// Seeking relative to end requires knowing the file size.
		// This is non-trivial with just a handle, would need GetFileSizeEx.
		// Returning error for simplicity here.
		// fileInfo, err := os.Stat(???) // Can't get path from handle easily
		// size, err := windows.GetFileSizeEx(f.Handle.h) // Requires windows call
		// if err != nil { return f.position, fmt.Errorf("seek: could not get file size: %w", err)}
		// newPos = size + offset
		return f.position, fmt.Errorf("seek: SeekEnd not implemented")
	default:
		return f.position, fmt.Errorf("seek: invalid whence %d", whence)
	}

	if newPos < 0 {
		return f.position, fmt.Errorf("seek: negative position %d", newPos)
	}
	f.position = newPos
	return f.position, nil
}
*/
