package edge

// GetStats returns packet statistics
type EdgeStats struct {
	TotalPacketsSent uint64
	TotalPacketsRecv uint64
	CommunityHash    uint32
}

// GetStats returns current packet statistics
func (e *EdgeClient) GetStats() EdgeStats {
	PacketsSent := e.PacketsSent.Load()
	PacketsRecv := e.PacketsRecv.Load()

	return EdgeStats{
		TotalPacketsSent: PacketsSent,
		TotalPacketsRecv: PacketsRecv,
		CommunityHash:    e.communityHash,
	}
}
