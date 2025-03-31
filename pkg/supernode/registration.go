package supernode

import (
	"fmt"
	"n2n-go/pkg/log"
	"n2n-go/pkg/machine"
	"n2n-go/pkg/p2p"
	"n2n-go/pkg/protocol"
	"n2n-go/pkg/protocol/netstruct"
	"net/netip"
	"time"
)

func (s *Supernode) RegisterCommunity(communityName string, communityHash uint32) (*Community, error) {
	chkHash := protocol.HashCommunity(communityName)
	if communityHash != chkHash {
		return nil, fmt.Errorf("wrong hash %d for community name: %s (expected %d)", communityHash, communityName, chkHash)
	}
	s.comMu.RLock()
	cm, exists := s.communities[communityHash]
	s.comMu.RUnlock()
	if exists {
		if cm.Name() == communityName {
			return cm, nil
		}
		return nil, fmt.Errorf("a different communityName already exists for hash %d (from name %s)", communityHash, communityName)
	}
	// Need to create, use write lock
	s.comMu.Lock()
	defer s.comMu.Unlock()

	if exists {
		if cm.Name() == communityName {
			return cm, nil
		}
		return nil, fmt.Errorf("a different communityName already exists for hash %d (from name %s)", communityHash, communityName)
	}

	// Create new community
	prefix, err := s.netAllocator.ProposeVirtualNetwork(communityName)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate network for community %s: %w", communityName, err)
	}

	cm, err = NewCommunityWithConfig(communityName, prefix, s.config, s)
	if err != nil {
		return nil, err
	}
	s.communities[communityHash] = cm

	return cm, nil
}

func (s *Supernode) GetCommunityForEdge(edgeMACAddr string, communityHash uint32) (*Community, error) {

	s.comMu.RLock()
	cm, exists := s.communities[communityHash]
	s.comMu.RUnlock()
	if !exists {
		return nil, ErrCommunityNotFound
	}

	_, ok := cm.GetEdge(edgeMACAddr)
	if !ok {
		return nil, fmt.Errorf("Community:%s addr %s: %w", cm.name, edgeMACAddr, ErrCommunityUnknownEdge)
	}
	return cm, nil

}

func (s *Supernode) GetCommunity(i EdgeAddressable) (*Community, error) {
	return s.GetCommunityForEdge(i.EdgeMACAddr(), i.CommunityHash())
}

// RegisterEdge registers or updates an edge in the supernode
func (s *Supernode) RegisterEdge(regMsg *protocol.Message[*netstruct.RegisterRequest]) (*Edge, *Community, error) {

	machineID, err := s.ValidateEdgeClaimedMACAddr(regMsg.EdgeMACAddr(), regMsg.Msg.EncryptedMachineID, regMsg.Msg.CommunityName)
	if err != nil {
		return nil, nil, err
	}
	regMsg.Msg.ClearMachineID = machineID

	cm, err := s.RegisterCommunity(regMsg.Msg.CommunityName, regMsg.CommunityHash())
	if err != nil {
		return nil, nil, err
	}

	// Update edge in community scope
	edge, err := cm.EdgeUpdate(regMsg)
	if err != nil {
		return nil, nil, err
	}

	s.stats.EdgesRegistered.Add(1)

	return edge, cm, nil
}

// UnregisterEdge removes an edge from the supernode
func (s *Supernode) UnregisterEdge(ea EdgeAddressable) error {

	cm, err := s.GetCommunity(ea)
	if err != nil {
		log.Printf("Supernode: error while unregistering edge %v for community %v: %v", ea.EdgeMACAddr(), ea.CommunityHash(), err)
		return err
	}

	if cm.Unregister(ea.EdgeMACAddr()) {
		s.onEdgeUnregistered(cm, ea.EdgeMACAddr())
	}
	return nil
}

func (s *Supernode) ValidateEdgeClaimedMACAddr(claimedMAC string, encryptedID []byte, communityName string) ([]byte, error) {

	machineID, err := s.DecryptMachineID(encryptedID)
	if err != nil {
		return nil, err
	}
	computedMAC, err := machine.ExtGenerateMac(communityName, machineID)
	if err != nil {
		return nil, err
	}
	computedClaimableMAC := computedMAC.String()
	if computedClaimableMAC != claimedMAC {
		return nil, fmt.Errorf("computedMac from shared secret (%s) doesn't match claimed mac (%s)", computedClaimableMAC, claimedMAC)
	}
	return machineID, nil
}

func (s *Supernode) onEdgeUnregistered(cm *Community, edgeMACAddr string) {
	s.edgeMu.Lock()
	edge := s.edgesByMAC[edgeMACAddr]
	pil := newPeerInfoEvent(p2p.TypeUnregister, edge)
	delete(s.edgesBySocket, edge.UDPAddr().String())
	delete(s.edgesByMAC, edgeMACAddr)
	s.edgeMu.Unlock()
	s.stats.EdgesUnregistered.Add(1)
	s.BroadcastStruct(pil, cm, s.MacADDR(), nil, edgeMACAddr)

}

func (s *Supernode) SetEdgeCachedInfo(macAddr string, desc string, community string, isRegistered bool, vip netip.Addr) {
	s.edgeCacheMu.Lock()
	defer s.edgeCacheMu.Unlock()
	s.edgeCachedInfos[macAddr] = &EdgeCachedInfos{
		MACAddr:      macAddr,
		Desc:         desc,
		Community:    community,
		IsRegistered: isRegistered,
		UpdatedAt:    time.Now(),
		VirtualIP:    vip,
	}
}

func (s *Supernode) GetEdgeCachedInfo(macAddr string) (*EdgeCachedInfos, bool) {
	s.edgeCacheMu.RLock()
	defer s.edgeCacheMu.RUnlock()
	infos, ok := s.edgeCachedInfos[macAddr]
	return infos, ok
}

func (s *Supernode) GetOfflineCachedEdges(cm *Community) (map[string]p2p.PeerCachedInfo, error) {
	if cm == nil {
		return nil, fmt.Errorf("nil community error")
	}
	communityName := cm.Name()
	offlineEdges := make(map[string]p2p.PeerCachedInfo)
	s.edgeCacheMu.RLock()
	defer s.edgeCacheMu.RUnlock()
	for _, v := range s.edgeCachedInfos {
		if v.Community != communityName || v.IsRegistered {
			continue
		}
		offlineEdges[v.MACAddr] = p2p.PeerCachedInfo{
			Desc:       v.Desc,
			MACAddr:    v.MACAddr,
			Community:  communityName,
			LastUpdate: v.UpdatedAt,
			VirtualIP:  v.VirtualIP,
		}
	}
	return offlineEdges, nil
}
func newPeerInfoEvent(eventType p2p.PeerInfoEventType, edge *Edge) *p2p.PeerInfoList {
	return &p2p.PeerInfoList{
		PeerInfos: []p2p.PeerInfo{
			edge.PeerInfo(),
		},
		EventType: eventType,
	}
}
