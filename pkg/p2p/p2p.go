package p2p

import (
	"fmt"
	"log"
	"n2n-go/pkg/protocol/spec"
	"net"
	"sync"
	"time"
)

type UDPWriteStrategy uint8

const (
	UDPEnforceSupernode UDPWriteStrategy = 0 //enforce relaying packet through supernode (e.g. for SUPER directed control messages)
	UDPBestEffort       UDPWriteStrategy = 1 //will send to Peer Socket if Peerlist Checked P2PAvailable
	UDPEnforceP2P       UDPWriteStrategy = 2 //will enforce PeerSocket whatever the state (e.g. for P2PAvailability check routine)
)

type P2PCapacity uint8

const (
	P2PUnknown     P2PCapacity = 0
	P2PPending     P2PCapacity = 1
	P2PAvailable   P2PCapacity = 2
	P2PUnavailable P2PCapacity = 3
)

func (pt P2PCapacity) String() string {
	switch pt {
	case P2PUnknown:
		return "Unknown"
	case P2PPending:
		return "Pending"
	case P2PAvailable:
		return "Available"
	case P2PUnavailable:
		return "Unavailable"
	default:
		return "Unknown"
	}
}

type PeerP2PInfos struct {
	From *Peer
	To   []*Peer
}

func (psi *PeerP2PInfos) PacketType() spec.PacketType {
	return spec.TypeP2PStateInfo
}

type P2PFullState struct {
	CommunityName string
	IsRequest     bool
	FullState     map[string]PeerP2PInfos
}

func (pfs *P2PFullState) PacketType() spec.PacketType {
	return spec.TypeP2PFullState
}

/*
func (pfs *P2PFullState) Encode() ([]byte, error) {
	return codec.NewCodec[P2PFullState]().Encode(*pfs)
}

func ParseP2PFullState(data []byte) (*P2PFullState, error) {
	return codec.NewCodec[P2PFullState]().Decode(data)
}
*/
/*
func (pfs *PeerP2PInfos) Encode() ([]byte, error) {
	return codec.NewCodec[PeerP2PInfos]().Encode(*pfs)
}

func ParsePeerP2PInfos(data []byte) (*PeerP2PInfos, error) {
	return codec.NewCodec[PeerP2PInfos]().Decode(data)
}
*/
type Peer struct {
	Infos      PeerInfo
	P2PStatus  P2PCapacity
	P2PCheckID string
	pendingTTL int
	UpdatedAt  time.Time
}

func (p *Peer) resetPendingTTL() {
	p.pendingTTL = 30
}

type PeerRegistry struct {
	peerMu                sync.RWMutex
	Me                    *Peer
	Peers                 map[string]*Peer //keyed by MACAddr.String()
	FullState             map[string]PeerP2PInfos
	IsWaitingForFullState bool
}

func NewPeerRegistry() *PeerRegistry {
	return &PeerRegistry{
		Peers: make(map[string]*Peer),
	}
}

func (reg *PeerRegistry) GetPeerP2PInfos() *PeerP2PInfos {
	var to []*Peer
	for _, v := range reg.Peers {
		to = append(to, v)
	}
	return &PeerP2PInfos{
		From: reg.Me,
		To:   to,
	}
}

func (reg *PeerRegistry) GetPeer(MACAddr string) (*Peer, error) {
	reg.peerMu.RLock()
	defer reg.peerMu.RUnlock()

	peer, exists := reg.Peers[MACAddr]
	if !exists {
		return nil, fmt.Errorf("peer with MAC address %s not found", MACAddr)
	}
	return peer, nil
}

func (p *Peer) UpdateP2PStatus(status P2PCapacity, checkid string) {
	var forcedStatement string
	skipLog := false
	if p.P2PStatus == status {
		skipLog = true
		if status == P2PPending {
			p.pendingTTL = p.pendingTTL - 1
		}
	} else {
		if p.P2PStatus == P2PUnknown && status == P2PPending {
			p.resetPendingTTL()
		}
	}
	if p.pendingTTL < 1 {
		skipLog = false
		forcedStatement = fmt.Sprintf("| Forcefull update, hole-punching TTL<0")
		p.P2PCheckID = ""
		p.P2PStatus = P2PUnavailable
	} else {
		p.P2PCheckID = checkid
		p.P2PStatus = status
	}
	p.UpdatedAt = time.Now()
	if !skipLog {
		log.Printf("Peers: Updated peer desc=%s vip=%s mac=%s with P2PStatus=%s %s", p.Infos.Desc, p.Infos.VirtualIP.String(), p.Infos.MACAddr.String(), p.P2PStatus.String(), forcedStatement)
	}
}

func (reg *PeerRegistry) AddPeer(infos PeerInfo, overwrite bool) (*Peer, error) {
	reg.peerMu.Lock()
	defer reg.peerMu.Unlock()

	macAddr := infos.MACAddr.String()
	if existingPeer, exists := reg.Peers[macAddr]; exists {
		origPeer := *existingPeer
		if !overwrite {
			return nil, fmt.Errorf("peer with MAC address %s already exists", macAddr)
		}
		if existingPeer.Infos.VirtualIP != infos.VirtualIP ||
			existingPeer.Infos.PubSocket.String() != infos.PubSocket.String() {
			log.Printf("Peers: peer with MAC %s updated with network difference: resetting P2PStatus", macAddr)
			existingPeer.P2PStatus = P2PUnknown
			existingPeer.P2PCheckID = ""
		}
		existingPeer.Infos = infos
		existingPeer.UpdatedAt = time.Now()
		log.Printf("Peers: updated known holde of %s MACAddr:", macAddr)
		log.Printf("  was: vip=%s PubSocket=%s desc=%s", origPeer.Infos.VirtualIP, origPeer.Infos.PubSocket, origPeer.Infos.Desc)
		log.Printf("  now: vip=%s PubSocket=%s desc=%s", existingPeer.Infos.VirtualIP, existingPeer.Infos.PubSocket, existingPeer.Infos.Desc)
		return existingPeer, nil
	}

	peer := &Peer{
		Infos:     infos,
		P2PStatus: P2PUnknown,
		UpdatedAt: time.Now(),
	}
	reg.Peers[macAddr] = peer
	log.Printf("Peers:   Added peer with desc=%s vip=%s mac=%s", peer.Infos.Desc, peer.Infos.VirtualIP.String(), peer.Infos.MACAddr.String())
	log.Printf("                         PubSocket=%s", peer.Infos.PubSocket)
	return peer, nil
}

func (reg *PeerRegistry) RemovePeer(MACAddr string) error {
	reg.peerMu.Lock()
	defer reg.peerMu.Unlock()

	p, exists := reg.Peers[MACAddr]
	if !exists {
		return fmt.Errorf("peer with MAC address %s not found", MACAddr)
	}

	dDesc := p.Infos.Desc
	dVip := p.Infos.VirtualIP.String()
	delete(reg.Peers, MACAddr)
	log.Printf("Peers: Removed peer with desc=%s vip=%s mac=%s", dDesc, dVip, MACAddr)
	return nil
}

func (p *Peer) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{
		IP:   p.Infos.PubSocket.IP,
		Port: p.Infos.PubSocket.Port,
	}
}

func (reg *PeerRegistry) GetP2PUnknownPeers() []*Peer {
	var peerlist []*Peer
	for _, p := range reg.Peers {
		if p.P2PStatus == P2PUnknown {
			peerlist = append(peerlist, p)
		}
	}
	return peerlist
}

func (reg *PeerRegistry) GetP2PendingPeers() []*Peer {
	var peerlist []*Peer
	for _, p := range reg.Peers {
		if p.P2PStatus == P2PPending {
			if p.pendingTTL > 0 {
				peerlist = append(peerlist, p)
			} else {
				p.UpdateP2PStatus(P2PUnavailable, "")
			}
		}
	}
	return peerlist
}

func (reg *PeerRegistry) GetP2PAvailablePeers() []*Peer {
	var peerlist []*Peer
	for _, p := range reg.Peers {
		if p.P2PStatus == P2PAvailable {
			peerlist = append(peerlist, p)
		}
	}
	return peerlist
}

// HandlePeerInfoList processes a peer.PeerInfoList and updates the registry accordingly.
// Depending on the event type, it may override or populate the full registry (ListEvent),
// with an option to overwrite existing P2PStatuses, or it may add or delete peers.
func (reg *PeerRegistry) HandlePeerInfoList(peerInfoList *PeerInfoList, reset bool, overwrite bool) error {

	switch peerInfoList.EventType {
	case TypeList:
		if reset {
			reg.peerMu.Lock()
			reg.Peers = make(map[string]*Peer)
			reg.peerMu.Unlock()
			log.Println("Peers: Resetting peer registry")
		}
		if peerInfoList.HasOrigin {
			me := &Peer{
				Infos:     peerInfoList.Origin,
				UpdatedAt: time.Now(),
			}
			reg.Me = me
			log.Printf("Peers: setting self PeerInfo from Origin Peerlist in supernode")
		}
		for _, info := range peerInfoList.PeerInfos {
			_, err := reg.AddPeer(info, overwrite)
			if err != nil {
				return fmt.Errorf("failed to add peer: %v", err)
			}
		}
		if !reset {
			// Remove peers that are not in the new list
			newPeers := make(map[string]struct{})
			for _, info := range peerInfoList.PeerInfos {
				macAddr := info.MACAddr.String()
				newPeers[macAddr] = struct{}{}
			}

			for macAddr := range reg.Peers {
				if _, exists := newPeers[macAddr]; !exists {
					reg.RemovePeer(macAddr)
					log.Printf("Peers: Removed peer with MAC address %s not in new list", macAddr)
				}
			}
		}
	case TypeRegister:
		for _, info := range peerInfoList.PeerInfos {
			_, err := reg.AddPeer(info, overwrite)
			if err != nil {
				return fmt.Errorf("failed to add peer: %v", err)
			}
		}
	case TypeUnregister:
		for _, info := range peerInfoList.PeerInfos {
			macAddr := info.MACAddr.String()
			err := reg.RemovePeer(macAddr)
			if err != nil {
				return fmt.Errorf("failed to remove peer: %v", err)
			}
		}
	default:
		return fmt.Errorf("unknown event type: %v", peerInfoList.EventType)
	}
	return nil
}
