package machine

import (
	"encoding/binary"
	"fmt"
	"net"
)

var HASH_KEY = [16]byte{0xd3, 0x1e, 0x48, 0xfa, 0x90, 0xfe, 0x4b, 0x4c, 0x9d, 0xaf, 0xd5, 0xd7, 0xa1, 0xb1, 0x2e, 0x8a}

func notSipHash24(key []byte, seed [16]byte) uint64 {
	v0 := binary.LittleEndian.Uint64(seed[0:8])
	v1 := binary.LittleEndian.Uint64(seed[8:16])
	v2 := v0 ^ 0x736f6d6570736575
	v3 := v1 ^ 0x646f72616e646f6d

	for _, k := range key {
		m := uint64(k)
		v3 ^= m
		v0 += v1
		v2 += v3
		v1 = (v1 << 13) | (v1 >> (64 - 13))
		v3 = (v3 << 15) | (v3 >> (64 - 15))
		v0 ^= v3
		v2 ^= v1
		v1 += v2
		v3 += v0
		v2 = (v2 << 5) | (v2 >> (64 - 5))
		v0 = (v0 << 10) | (v0 >> (64 - 10))
		v3 ^= v1
		v2 ^= v0
	}

	v0 += v1
	v2 += v3
	v1 = (v1 << 13) | (v1 >> (64 - 13))
	v3 = (v3 << 15) | (v3 >> (64 - 15))
	v0 ^= v3
	v2 ^= v1
	v1 += v2
	v3 += v0
	v2 = (v2 << 5) | (v2 >> (64 - 5))
	v0 = (v0 << 10) | (v0 >> (64 - 10))
	v3 ^= v1
	v2 ^= v0

	return v0 ^ v1 ^ v2 ^ v3
}

func netGenerateMac(idString string, mac *net.HardwareAddr, hashKey [16]byte, idx uint64) error {
	machineIDBytes, err := GetMachineID()
	if err != nil {
		return err
	}

	nameBytes := []byte(idString)
	data := append(machineIDBytes, nameBytes...)
	if idx > 0 {
		idxBytes := make([]byte, 8)
		binary.LittleEndian.PutUint64(idxBytes, idx)
		data = append(data, idxBytes...)
	}

	result := notSipHash24(data, hashKey)

	macBytes := make(net.HardwareAddr, 8)
	binary.LittleEndian.PutUint64(macBytes, result)
	*mac = macBytes[:6]
	markRandomMAC(mac)
	(*mac)[0] &= 0xfe // clear multicast
	return nil
}
func markRandomMAC(mac *net.HardwareAddr) {
	if len(*mac) > 0 {
		(*mac)[0] |= 0x02 // Set the locally administered bit
	}
}
func GenerateMac(communityName string) (net.HardwareAddr, error) {
	var generatedMAC net.HardwareAddr
	err := netGenerateMac(communityName, &generatedMAC, HASH_KEY, 12345)
	if err != nil {
		return nil, fmt.Errorf("Error generating MAC: %v\n", err)
	}
	return generatedMAC, nil
}
