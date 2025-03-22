package ippool

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

// IPPool manages a pool of IP addresses and their leases.
type IPPool struct {
	ipNet         *net.IPNet
	ipRange       []net.IP
	availableIPs  map[string]bool  // IP address (string representation) to availability (true if available)
	leases        map[string]Lease // MAC address to Lease
	leaseDuration time.Duration
	mu            sync.Mutex
}

// Lease represents a lease for an IP address.
type Lease struct {
	IP        net.IP
	MAC       string
	Expiry    time.Time
	Sticky    bool // Indicates if this lease is sticky
	LastRenew time.Time
}

var (
	registeredPools   = make(map[string]*IPPool) // CIDR string as key
	registeredPoolsMu sync.Mutex
	db                *bbolt.DB
	dbPath            = "ippool.db"            // Default database file path
	dbRetryAttempts   = 3                      // Number of retry attempts for DB operations
	dbRetryDelay      = 100 * time.Millisecond // Delay between retries
)

const (
	poolsBucketName    = "pools"
	poolConfigBucket   = "config"
	availableIPsBucket = "available_ips"
	leasesBucket       = "leases"
)

// InitializeDB initializes the BoltDB database.
func InitializeDB(path string) error {
	if path != "" {
		dbPath = path
	}
	options := &bbolt.Options{Timeout: 1 * time.Second}
	var err error
	for i := 0; i < dbRetryAttempts; i++ { // Retry loop for opening DB
		db, err = bbolt.Open(dbPath, 0600, options)
		if err == nil {
			return nil // Success
		}
		fmt.Printf("Error opening database (attempt %d/%d): %v, retrying in %v\n", i+1, dbRetryAttempts, err, dbRetryDelay)
		time.Sleep(dbRetryDelay)
	}
	return fmt.Errorf("failed to open database after %d attempts: %w", dbRetryAttempts, err)
}

// CloseDB closes the BoltDB database.
func CloseDB() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

// LoadPoolState loads the IP pool state from the database with retry.
func LoadPoolState() error {
	for i := 0; i < dbRetryAttempts; i++ {
		err := loadPoolStateInternal()
		if err == nil {
			return nil // Success
		}
		fmt.Printf("Error loading pool state from DB (attempt %d/%d): %v, retrying in %v\n", i+1, dbRetryAttempts, err, dbRetryDelay)
		time.Sleep(dbRetryDelay)
	}
	return fmt.Errorf("failed to load pool state after %d attempts", dbRetryAttempts)
}

func loadPoolStateInternal() error {
	registeredPoolsMu.Lock()
	defer registeredPoolsMu.Unlock()

	return db.View(func(tx *bbolt.Tx) error {
		poolsBucket := tx.Bucket([]byte(poolsBucketName))
		if poolsBucket == nil {
			return nil // No pools bucket yet, fresh start
		}

		return poolsBucket.ForEach(func(cidrBytes, _ []byte) error {
			cidr := string(cidrBytes)
			poolBucket := poolsBucket.Bucket(cidrBytes)
			if poolBucket == nil {
				return fmt.Errorf("pool bucket not found for CIDR: %s", cidr)
			}

			configBytes := poolBucket.Get([]byte(poolConfigBucket))
			if configBytes == nil {
				return fmt.Errorf("pool config not found for CIDR: %s", cidr)
			}
			poolConfig, err := decodePoolConfig(configBytes)
			if err != nil {
				return fmt.Errorf("failed to decode pool config for CIDR %s: %w", cidr, err)
			}

			ipNet, err := parseCIDR(poolConfig.CIDR)
			if err != nil {
				return fmt.Errorf("invalid CIDR loaded from DB: %w", err)
			}
			ips, err := generateIPsFromCIDR(ipNet)
			if err != nil {
				return fmt.Errorf("error generating IPs from CIDR loaded from DB: %w", err)
			}

			pool := &IPPool{
				ipNet:         ipNet,
				ipRange:       ips,
				availableIPs:  make(map[string]bool),  // Will be populated from DB
				leases:        make(map[string]Lease), // Will be populated from DB
				leaseDuration: poolConfig.LeaseDuration,
				mu:            sync.Mutex{},
			}

			// Load available IPs
			availableIPsBucket := poolBucket.Bucket([]byte(availableIPsBucket))
			if availableIPsBucket != nil {
				err := availableIPsBucket.ForEach(func(ipBytes, availableBytes []byte) error {
					available, err := decodeBool(availableBytes)
					if err != nil {
						return fmt.Errorf("failed to decode available IP status for IP %s in CIDR %s: %w", string(ipBytes), cidr, err)
					}
					pool.availableIPs[string(ipBytes)] = available
					return nil
				})
				if err != nil {
					return fmt.Errorf("error iterating available IPs bucket for CIDR %s: %w", cidr, err)
				}
			} else {
				// If no available_ips bucket, assume all IPs are available initially (fresh pool load)
				for _, ip := range ips {
					pool.availableIPs[ip.String()] = true
				}
			}

			// Load leases
			leasesBucket := poolBucket.Bucket([]byte(leasesBucket))
			if leasesBucket != nil {
				err := leasesBucket.ForEach(func(macBytes, leaseBytes []byte) error {
					lease, err := decodeLease(leaseBytes)
					if err != nil {
						return fmt.Errorf("failed to decode lease for MAC %s in CIDR %s: %w", string(macBytes), cidr, err)
					}
					pool.leases[string(macBytes)] = lease
					return nil
				})
				if err != nil {
					return fmt.Errorf("error iterating leases bucket for CIDR %s: %w", cidr, err)
				}
			}

			registeredPools[cidr] = pool // Register the loaded pool
			return nil
		})
	})
}

// SavePoolState saves the IP pool state to the database with retry.
func SavePoolState() error {
	for i := 0; i < dbRetryAttempts; i++ {
		err := savePoolStateInternal()
		if err == nil {
			return nil // Success
		}
		fmt.Printf("Error saving pool state to DB (attempt %d/%d): %v, retrying in %v\n", i+1, dbRetryAttempts, err, dbRetryDelay)
		time.Sleep(dbRetryDelay)
	}
	return fmt.Errorf("failed to save pool state after %d attempts", dbRetryAttempts)
}

func savePoolStateInternal() error {
	registeredPoolsMu.Lock()
	defer registeredPoolsMu.Unlock()

	return db.Update(func(tx *bbolt.Tx) error {
		poolsBucket, err := tx.CreateBucketIfNotExists([]byte(poolsBucketName))
		if err != nil {
			return fmt.Errorf("failed to create pools bucket: %w", err)
		}

		for cidr, pool := range registeredPools {
			poolBucket, err := poolsBucket.CreateBucketIfNotExists([]byte(cidr))
			if err != nil {
				return fmt.Errorf("failed to create pool bucket for CIDR %s: %w", cidr, err)
			}

			// Save pool config
			config := PersistedPoolConfig{
				CIDR:          cidr,
				LeaseDuration: pool.leaseDuration,
			}
			configBytes, err := encodePoolConfig(config)
			if err != nil {
				return fmt.Errorf("failed to encode pool config for CIDR %s: %w", cidr, err)
			}
			if err := poolBucket.Put([]byte(poolConfigBucket), configBytes); err != nil {
				return fmt.Errorf("failed to put pool config for CIDR %s: %w", cidr, err)
			}

			// Save available IPs
			availableIPsBucket, err := poolBucket.CreateBucketIfNotExists([]byte(availableIPsBucket))
			if err != nil {
				return fmt.Errorf("failed to create available IPs bucket for CIDR %s: %w", cidr, err)
			}
			if err := availableIPsBucket.ForEach(func(k, _ []byte) error {
				return availableIPsBucket.Delete(k) // Clear existing data
			}); err != nil {
				return fmt.Errorf("failed to clear available IPs bucket for CIDR %s: %w", cidr, err)
			}
			for ipStr, available := range pool.availableIPs {
				availableBytes, err := encodeBool(available)
				if err != nil {
					return fmt.Errorf("failed to encode available IP status for IP %s in CIDR %s: %w", ipStr, cidr, err)
				}
				if err := availableIPsBucket.Put([]byte(ipStr), availableBytes); err != nil {
					return fmt.Errorf("failed to put available IP status for IP %s in CIDR %s: %w", ipStr, cidr, err)
				}
			}

			// Save leases
			leasesBucket, err := poolBucket.CreateBucketIfNotExists([]byte(leasesBucket))
			if err != nil {
				return fmt.Errorf("failed to create leases bucket for CIDR %s: %w", cidr, err)
			}
			if err := leasesBucket.ForEach(func(k, _ []byte) error {
				return leasesBucket.Delete(k) // Clear existing data
			}); err != nil {
				return fmt.Errorf("failed to clear leases bucket for CIDR %s: %w", cidr, err)
			}
			for mac, lease := range pool.leases {
				leaseBytes, err := encodeLease(lease)
				if err != nil {
					return fmt.Errorf("failed to encode lease for MAC %s in CIDR %s: %w", mac, cidr, err)
				}
				if err := leasesBucket.Put([]byte(mac), leaseBytes); err != nil {
					return fmt.Errorf("failed to put lease for MAC %s in CIDR %s: %w", mac, cidr, err)
				}
			}
		}
		return nil
	})
}

// PersistedPoolConfig is a struct to hold IPPool configuration for persistence.
type PersistedPoolConfig struct {
	CIDR          string
	LeaseDuration time.Duration
}

// PersistedLease is a struct to hold Lease data for persistence.
type PersistedLease struct {
	IP        net.IP
	MAC       string
	Expiry    time.Time
	Sticky    bool
	LastRenew time.Time
}

// --- Encoding and Decoding Functions ---

func encodePoolConfig(config PersistedPoolConfig) ([]byte, error) {
	buf := new(bytes.Buffer)

	if err := binary.Write(buf, binary.BigEndian, uint64(len(config.CIDR))); err != nil {
		return nil, fmt.Errorf("encode CIDR length: %w", err)
	}
	if _, err := buf.WriteString(config.CIDR); err != nil {
		return nil, fmt.Errorf("encode CIDR string: %w", err)
	}
	if err := binary.Write(buf, binary.BigEndian, config.LeaseDuration.Nanoseconds()); err != nil {
		return nil, fmt.Errorf("encode LeaseDuration: %w", err)
	}
	return buf.Bytes(), nil
}

func decodePoolConfig(data []byte) (*PersistedPoolConfig, error) {
	buf := bytes.NewReader(data)
	config := PersistedPoolConfig{}

	var cidrLen uint64
	if err := binary.Read(buf, binary.BigEndian, &cidrLen); err != nil {
		return nil, fmt.Errorf("decode CIDR length: %w", err)
	}
	cidrBytes := make([]byte, cidrLen)
	if _, err := buf.Read(cidrBytes); err != nil {
		return nil, fmt.Errorf("decode CIDR string: %w", err)
	}
	config.CIDR = string(cidrBytes)

	var leaseDurationNanos int64
	if err := binary.Read(buf, binary.BigEndian, &leaseDurationNanos); err != nil {
		return nil, fmt.Errorf("decode LeaseDuration: %w", err)
	}
	config.LeaseDuration = time.Duration(leaseDurationNanos)
	return &config, nil
}

func encodeLease(lease Lease) ([]byte, error) {
	buf := new(bytes.Buffer)

	ipBytes := lease.IP.To4()
	if ipBytes == nil {
		return nil, fmt.Errorf("invalid IPv4 address in lease")
	}
	if _, err := buf.Write(ipBytes); err != nil { // 4 bytes for IPv4
		return nil, fmt.Errorf("encode IP: %w", err)
	}

	if err := binary.Write(buf, binary.BigEndian, uint64(len(lease.MAC))); err != nil {
		return nil, fmt.Errorf("encode MAC length: %w", err)
	}
	if _, err := buf.WriteString(lease.MAC); err != nil {
		return nil, fmt.Errorf("encode MAC string: %w", err)
	}
	if err := binary.Write(buf, binary.BigEndian, lease.Expiry.UnixNano()); err != nil {
		return nil, fmt.Errorf("encode Expiry: %w", err)
	}
	if err := binary.Write(buf, binary.BigEndian, lease.Sticky); err != nil {
		return nil, fmt.Errorf("encode Sticky: %w", err)
	}
	if err := binary.Write(buf, binary.BigEndian, lease.LastRenew.UnixNano()); err != nil {
		return nil, fmt.Errorf("encode LastRenew: %w", err)
	}

	return buf.Bytes(), nil
}

func decodeLease(data []byte) (Lease, error) {
	lease := Lease{}
	buf := bytes.NewReader(data)

	ipBytes := make([]byte, net.IPv4len)
	if _, err := buf.Read(ipBytes); err != nil {
		return lease, fmt.Errorf("decode IP: %w", err)
	}
	lease.IP = net.IPv4(ipBytes[0], ipBytes[1], ipBytes[2], ipBytes[3])

	var macLen uint64
	if err := binary.Read(buf, binary.BigEndian, &macLen); err != nil {
		return lease, fmt.Errorf("decode MAC length: %w", err)
	}
	macBytes := make([]byte, macLen)
	if _, err := buf.Read(macBytes); err != nil {
		return lease, fmt.Errorf("decode MAC string: %w", err)
	}
	lease.MAC = string(macBytes)

	var expiryNanos int64
	if err := binary.Read(buf, binary.BigEndian, &expiryNanos); err != nil {
		return lease, fmt.Errorf("decode Expiry: %w", err)
	}
	lease.Expiry = time.Unix(0, expiryNanos)

	if err := binary.Read(buf, binary.BigEndian, &lease.Sticky); err != nil {
		return lease, fmt.Errorf("decode Sticky: %w", err)
	}
	var lastRenewNanos int64
	if err := binary.Read(buf, binary.BigEndian, &lastRenewNanos); err != nil {
		return lease, fmt.Errorf("decode LastRenew: %w", err)
	}
	lease.LastRenew = time.Unix(0, lastRenewNanos)

	return lease, nil
}

func encodeBool(value bool) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, value)
	if err != nil {
		return nil, fmt.Errorf("encode bool: %w", err)
	}
	return buf.Bytes(), nil
}

func decodeBool(data []byte) (bool, error) {
	var value bool
	buf := bytes.NewReader(data)
	err := binary.Read(buf, binary.BigEndian, &value)
	if err != nil {
		return false, fmt.Errorf("decode bool: %w", err)
	}
	return value, nil
}

// NewIPPool, UnregisterIPPool, deletePoolFromDB, parseCIDR, generateIPsFromCIDR, lastIP, ipToInt, intToIP, cidrOverlaps, RequestIP, RenewLease, ReleaseLease, GetLease, CleanupExpiredLeases (remain mostly the same, but call the internal Save/Load and use binary encode/decode)
// ... (rest of the code - NewIPPool, UnregisterIPPool, deletePoolFromDB, parseCIDR, generateIPsFromCIDR, lastIP, ipToInt, intToIP, cidrOverlaps, RequestIP, RenewLease, ReleaseLease, GetLease, CleanupExpiredLeases functions from previous version - adapt to use binary encoding and retry in Save/Load)

// NewIPPool creates a new IP pool from a CIDR string (e.g., "192.168.1.0/24")
// and a default lease duration.
func NewIPPool(cidr string, leaseDuration time.Duration) (*IPPool, error) {
	ipNet, err := parseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR: %w", err)
	}

	registeredPoolsMu.Lock()
	defer registeredPoolsMu.Unlock()

	// Check for overlap with existing pools
	for registeredCIDR, _ := range registeredPools {
		if cidrOverlaps(cidr, registeredCIDR) {
			return nil, fmt.Errorf("CIDR range overlaps with existing pool: %s", registeredCIDR)
		}
	}

	ips, err := generateIPsFromCIDR(ipNet)
	if err != nil {
		return nil, fmt.Errorf("error generating IPs from CIDR: %w", err)
	}

	availableIPs := make(map[string]bool)
	for _, ip := range ips {
		availableIPs[ip.String()] = true
	}

	pool := &IPPool{
		ipNet:         ipNet,
		ipRange:       ips,
		availableIPs:  availableIPs,
		leases:        make(map[string]Lease),
		leaseDuration: leaseDuration,
		mu:            sync.Mutex{},
	}
	registeredPools[cidr] = pool // Register the new pool

	// Persist the new pool to DB
	if err := saveNewPoolToDB(pool, cidr); err != nil {
		// In case of DB error after pool creation, you might want to rollback registration
		delete(registeredPools, cidr)
		return nil, fmt.Errorf("failed to save new pool to DB: %w", err)
	}

	return pool, nil
}

func saveNewPoolToDB(pool *IPPool, cidr string) error {
	return db.Update(func(tx *bbolt.Tx) error {
		poolsBucket, err := tx.CreateBucketIfNotExists([]byte(poolsBucketName))
		if err != nil {
			return fmt.Errorf("failed to create pools bucket: %w", err)
		}
		poolBucket, err := poolsBucket.CreateBucketIfNotExists([]byte(cidr))
		if err != nil {
			return fmt.Errorf("failed to create pool bucket for CIDR %s: %w", cidr, err)
		}

		// Save pool config
		config := PersistedPoolConfig{
			CIDR:          cidr,
			LeaseDuration: pool.leaseDuration,
		}
		configBytes, err := encodePoolConfig(config)
		if err != nil {
			return fmt.Errorf("failed to encode pool config for CIDR %s: %w", cidr, err)
		}
		if err := poolBucket.Put([]byte(poolConfigBucket), configBytes); err != nil {
			return fmt.Errorf("failed to put pool config for CIDR %s: %w", cidr, err)
		}
		return nil
	})
}

// UnregisterIPPool unregisters an IP pool associated with the given CIDR.
// After unregistering, the IPs in this CIDR can be used in new IP pools.
func UnregisterIPPool(cidr string) error {
	registeredPoolsMu.Lock()
	defer registeredPoolsMu.Unlock()

	if _, exists := registeredPools[cidr]; !exists {
		return fmt.Errorf("IP pool with CIDR '%s' not registered", cidr)
	}
	delete(registeredPools, cidr)

	// Remove pool from DB
	if err := deletePoolFromDB(cidr); err != nil {
		return fmt.Errorf("failed to delete pool from DB: %w", err)
	}

	return nil
}

func deletePoolFromDB(cidr string) error {
	return db.Update(func(tx *bbolt.Tx) error {
		poolsBucket := tx.Bucket([]byte(poolsBucketName))
		if poolsBucket != nil {
			return poolsBucket.DeleteBucket([]byte(cidr))
		}
		return nil
	})
}

// parseCIDR, generateIPsFromCIDR, lastIP, ipToInt, intToIP, cidrOverlaps  (No changes needed from previous version)
// ... (parseCIDR, generateIPsFromCIDR, lastIP, ipToInt, intToIP, cidrOverlaps functions from previous version - remain the same)
func parseCIDR(cidr string) (*net.IPNet, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	if ipNet.IP.To4() == nil {
		return nil, fmt.Errorf("only IPv4 CIDR ranges are supported")
	}
	return ipNet, nil
}

// generateIPsFromCIDR generates a slice of net.IP addresses from a net.IPNet.
func generateIPsFromCIDR(ipNet *net.IPNet) ([]net.IP, error) {
	var ips []net.IP
	startIP := ipNet.IP.Mask(ipNet.Mask) // Network address
	endIP := lastIP(ipNet)

	if ipToInt(startIP) > ipToInt(endIP) {
		return nil, fmt.Errorf("invalid CIDR range")
	}

	for ipInt := ipToInt(startIP); ipInt <= ipToInt(endIP); ipInt++ {
		ip := intToIP(ipInt)
		// Exclude network and broadcast addresses (if applicable for the CIDR)
		if !ip.Equal(startIP) && !ip.Equal(endIP) { // Basic check, might need more robust network/broadcast detection for all CIDR sizes if required
			ips = append(ips, ip)
		}
	}
	return ips, nil
}

// lastIP returns the last IP address in a net.IPNet.
func lastIP(ipNet *net.IPNet) net.IP {
	ip := ipNet.IP.To4()
	mask := ipNet.Mask
	lastIP := net.IP(make(net.IP, len(ip)))
	for i := 0; i < len(ip); i++ {
		lastIP[i] = ip[i] | ^mask[i]
	}
	return lastIP
}

// ipToInt converts net.IP to uint32.
func ipToInt(ip net.IP) uint32 {
	ip4 := ip.To4()
	if ip4 == nil {
		return 0 // Not an IPv4 address
	}
	return uint32(ip4[0])<<24 | uint32(ip4[1])<<16 | uint32(ip4[2])<<8 | uint32(ip4[3])
}

// intToIP converts uint32 to net.IP.
func intToIP(ipInt uint32) net.IP {
	return net.IPv4(byte(ipInt>>24), byte(ipInt>>16), byte(ipInt>>8), byte(ipInt))
}

// cidrOverlaps checks if two CIDR ranges overlap.
func cidrOverlaps(cidr1 string, cidr2 string) bool {
	_, ipNet1, err1 := net.ParseCIDR(cidr1)
	_, ipNet2, err2 := net.ParseCIDR(cidr2)

	if err1 != nil || err2 != nil {
		// Handle error appropriately, for now assume no overlap if parsing fails
		return false
	}

	if ipNet1.Contains(ipNet2.IP) || ipNet2.Contains(ipNet1.IP) {
		return true
	}
	startIP1 := ipNet1.IP.Mask(ipNet1.Mask)
	endIP1 := lastIP(ipNet1)
	startIP2 := ipNet2.IP.Mask(ipNet2.Mask)
	endIP2 := lastIP(ipNet2)

	return ipToInt(endIP1) >= ipToInt(startIP2) && ipToInt(endIP2) >= ipToInt(startIP1)
}

// RequestIP requests an IP address for a given MAC address.
// 'sticky' parameter determines if a sticky lease is requested.
func (p *IPPool) RequestIP(mac string, sticky bool) (net.IP, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()

	// 1. Check if there's an existing lease for this MAC
	if lease, ok := p.leases[mac]; ok {
		if lease.Expiry.After(now) { // Lease is still valid, renew it
			lease.Expiry = now.Add(p.leaseDuration)
			lease.LastRenew = now
			p.leases[mac] = lease                   // Update the lease with new expiry
			if err := SavePoolState(); err != nil { // Persist state after renew
				fmt.Println("Error saving pool state after lease renew:", err) // Log error but don't block request
			}
			return lease.IP, nil
		} else { // Lease expired
			if sticky || lease.Sticky { // Sticky requested (now or before)
				if p.availableIPs[lease.IP.String()] { // Sticky requested and IP is available, re-assign
					p.availableIPs[lease.IP.String()] = false // Mark as taken again
					newLease := Lease{
						IP:        lease.IP,
						MAC:       mac,
						Expiry:    now.Add(p.leaseDuration),
						Sticky:    sticky || lease.Sticky, // Keep sticky if it was before or requested now
						LastRenew: now,
					}
					p.leases[mac] = newLease
					if err := SavePoolState(); err != nil { // Persist state after sticky re-assign
						fmt.Println("Error saving pool state after sticky re-assign:", err) // Log error
					}
					return lease.IP, nil
				} else {
					// Sticky requested, but IP NOT available (someone else took it due to pool starvation)
					// In this case, we cannot honor the sticky request for this IP.
					// We fall through to allocate a new IP if possible. Lease is still in 'leases' but effectively lost its stickiness for now in terms of immediate reuse.
					fmt.Printf("Sticky lease IP %s for MAC %s not available, allocating new IP if possible.\n", lease.IP, mac)
				}
			} else {
				// Non-sticky expired lease: release the IP back to the pool.
				p.availableIPs[lease.IP.String()] = true // Release the IP back to pool
				delete(p.leases, mac)                    // Remove the expired lease
			}
		}
	}

	// 2. No existing valid lease (or sticky re-assignment failed due to IP being unavailable), find a new available IP
	var assignedIP net.IP
	for _, ip := range p.ipRange {
		ipStr := ip.String()
		if p.availableIPs[ipStr] {
			assignedIP = ip
			p.availableIPs[ipStr] = false // Mark as taken
			break                         // Found an available IP
		}
	}

	if assignedIP == nil {
		return nil, fmt.Errorf("IP pool exhausted")
	}

	newLease := Lease{
		IP:        assignedIP,
		MAC:       mac,
		Expiry:    now.Add(p.leaseDuration),
		Sticky:    sticky,
		LastRenew: now,
	}
	p.leases[mac] = newLease
	if err := SavePoolState(); err != nil { // Persist state after new lease
		fmt.Println("Error saving pool state after new lease:", err) // Log error
	}
	return assignedIP, nil
}

// RenewLease renews the lease for a given MAC address.
func (p *IPPool) RenewLease(mac string) (net.IP, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	lease, ok := p.leases[mac]
	if !ok {
		return nil, fmt.Errorf("no lease found for MAC address: %s", mac)
	}

	lease.Expiry = time.Now().Add(p.leaseDuration)
	lease.LastRenew = time.Now()
	p.leases[mac] = lease                   // Update the lease with new expiry
	if err := SavePoolState(); err != nil { // Persist state after renew
		fmt.Println("Error saving pool state after lease renew:", err) // Log error
	}
	return lease.IP, nil
}

// ReleaseLease releases the lease for a given MAC address, making the IP available again.
func (p *IPPool) ReleaseLease(mac string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	lease, ok := p.leases[mac]
	if !ok {
		return fmt.Errorf("no lease found for MAC address: %s", mac)
	}

	p.availableIPs[lease.IP.String()] = true // Mark IP as available
	delete(p.leases, mac)                    // Remove the lease
	if err := SavePoolState(); err != nil {  // Persist state after release
		fmt.Println("Error saving pool state after lease release:", err) // Log error
	}
	return nil
}

// GetLease returns the lease information for a given MAC address, or nil if no lease exists.
func (p *IPPool) GetLease(mac string) *Lease {
	p.mu.Lock()
	defer p.mu.Unlock()
	lease, ok := p.leases[mac]
	if !ok {
		return nil
	}
	return &lease // Return a copy to avoid race conditions if caller modifies it without lock.
}

// CleanupExpiredLeases checks for expired leases and releases their IPs back to the pool.
// Modified to only clean up NON-STICKY expired leases. Sticky leases are kept expired.
func (p *IPPool) CleanupExpiredLeases() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	leasesChanged := false // Track if leases were changed to potentially save state only when needed.
	for mac, lease := range p.leases {
		if lease.Expiry.Before(now) {
			if !lease.Sticky { // Only cleanup non-sticky expired leases
				fmt.Printf("Non-sticky lease for MAC %s (IP: %s) expired, releasing IP.\n", mac, lease.IP)
				p.availableIPs[lease.IP.String()] = true // Make IP available again
				delete(p.leases, mac)                    // Remove the expired lease
				leasesChanged = true
			} else {
				fmt.Printf("Sticky lease for MAC %s (IP: %s) expired, keeping lease (expired).\n", mac, lease.IP)
				// Do NOT release IP for sticky leases.  They remain in 'leases' map.
				// They will be reused in RequestIP if the IP is still available when requested again.
			}
		}
	}
	if leasesChanged {
		if err := SavePoolState(); err != nil { // Persist state after cleanup
			fmt.Println("Error saving pool state after cleanup:", err) // Log error
		}
	}
}
