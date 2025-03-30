package protocol

import "net"

// special identifiers for edge cases

const (
	SNMacAddrString = "CA:FE:C0:FF:EE:00"
)

var (
	snMacAddr, _ = net.ParseMAC(SNMacAddrString)
)

func SupernodeMACAddr() net.HardwareAddr {
	return snMacAddr
}

func IsSNMacAddr(mac net.HardwareAddr) bool {
	return mac.String() == SupernodeMACAddr().String()
}

func IsSNMacAddrString(macStr string) bool {
	return macStr == SupernodeMACAddr().String()
}
