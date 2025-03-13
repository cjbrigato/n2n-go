package buffers

import (
	"sync"
)

const (
	// DefaultBufferSize is the default size for packet buffers
	// Large enough for protocol header + max Ethernet frame
	DefaultBufferSize = 2048

	// MaxPacketSize is the maximum packet size we expect to handle
	MaxPacketSize = 1500
)

// BufferPool maintains a pool of byte slices to reduce GC pressure
type BufferPool struct {
	pool sync.Pool
	size int
}

// NewBufferPool creates a new buffer pool with the specified buffer size
func NewBufferPool(size int) *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, size)
				return &buf
			},
		},
		size: size,
	}
}

// Get retrieves a buffer from the pool
func (p *BufferPool) Get() []byte {
	buffer := *(p.pool.Get().(*[]byte))

	// Ensure the buffer is properly sized and cleared
	if cap(buffer) < p.size {
		// Unlikely but possible if the buffer was resized
		buffer = make([]byte, p.size)
	} else {
		buffer = buffer[:p.size]
		// No need to zero the buffer - the caller should only read/write
		// the portions they explicitly fill
	}

	return buffer
}

// Put returns a buffer to the pool
func (p *BufferPool) Put(buffer []byte) {
	if buffer == nil || cap(buffer) < p.size {
		return // Don't keep undersized buffers
	}

	// Reset the buffer to the pool's standard size
	buffer = buffer[:p.size]
	p.pool.Put(&buffer)
}

// Global pool instances for common sizes
var (
	// PacketBufferPool for network packet buffers (header + payload)
	PacketBufferPool = NewBufferPool(DefaultBufferSize)

	// HeaderBufferPool for just protocol headers
	HeaderBufferPool = NewBufferPool(128)
)
