package edge

import (
	"fmt"
	"log"
	"n2n-go/pkg/peer"
	"net"
	"sync"
	"time"
)

type P2PCapacity uint8

const (
	P2PUnknown     P2PCapacity = 0
	P2PPending     P2PCapacity = 1
	P2PAvailable   P2PCapacity = 2
	P2PUnavailable P2PCapacity = 3
)

type Peer struct {
	Infos     peer.PeerInfo
	P2PStatus P2PCapacity
	UpdatedAt time.Time
}

type PeerRegistry struct {
	peerMu sync.RWMutex
	Peers  map[string]*Peer //keyed by MACAddr.String()
}

func NewPeerRegistry() *PeerRegistry {
	return &PeerRegistry{
		Peers: make(map[string]*Peer),
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

func (reg *PeerRegistry) AddPeer(infos peer.PeerInfo, overwrite bool) (*Peer, error) {
	reg.peerMu.Lock()
	defer reg.peerMu.Unlock()

	macAddr := infos.MACAddr.String()
	if existingPeer, exists := reg.Peers[macAddr]; exists {
		if !overwrite {
			return nil, fmt.Errorf("peer with MAC address %s already exists", macAddr)
		}
		if existingPeer.Infos.VirtualIP != infos.VirtualIP ||
			existingPeer.Infos.PubSocket.String() != infos.PubSocket.String() {
			existingPeer.P2PStatus = P2PUnknown
		}
		existingPeer.Infos = infos
		existingPeer.UpdatedAt = time.Now()
		log.Printf("Peers: Updated existing peer with MAC address %s", macAddr)
		return existingPeer, nil
	}

	peer := &Peer{
		Infos:     infos,
		P2PStatus: P2PUnknown,
		UpdatedAt: time.Now(),
	}
	reg.Peers[macAddr] = peer
	log.Printf("Peers: Added new peer with MAC address %s", macAddr)
	return peer, nil
}

func (reg *PeerRegistry) RemovePeer(MACAddr string) error {
	reg.peerMu.Lock()
	defer reg.peerMu.Unlock()

	if _, exists := reg.Peers[MACAddr]; !exists {
		return fmt.Errorf("peer with MAC address %s not found", MACAddr)
	}

	delete(reg.Peers, MACAddr)
	log.Printf("Peers: Removed peer with MAC address %s", MACAddr)
	return nil
}

func (p *Peer) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{
		IP:   p.Infos.PubSocket.IP,
		Port: p.Infos.PubSocket.Port,
	}
}

// HandlePeerInfoList processes a peer.PeerInfoList and updates the registry accordingly.
// Depending on the event type, it may override or populate the full registry (ListEvent),
// with an option to overwrite existing P2PStatuses, or it may add or delete peers.
func (reg *PeerRegistry) HandlePeerInfoList(peerInfoList *peer.PeerInfoList, reset bool, overwrite bool) error {
	reg.peerMu.Lock()
	defer reg.peerMu.Unlock()

	switch peerInfoList.EventType {
	case peer.TypeList:
		if reset {
			reg.Peers = make(map[string]*Peer)
			log.Println("Peers: Resetting peer registry")
		}
		for _, info := range peerInfoList.PeerInfos {
			_, err := reg.AddPeer(info, overwrite)
			if err != nil {
				return fmt.Errorf("failed to add peer: %v", err)
			}
		}
		// Remove peers that are not in the new list
		newPeers := make(map[string]struct{})
		for _, info := range peerInfoList.PeerInfos {
			macAddr := info.MACAddr.String()
			newPeers[macAddr] = struct{}{}
		}

		for macAddr := range reg.Peers {
			if _, exists := newPeers[macAddr]; !exists {
				delete(reg.Peers, macAddr)
				log.Printf("Peers: Removed peer with MAC address %s not in new list", macAddr)
			}
		}
	case peer.TypeRegister:
		for _, info := range peerInfoList.PeerInfos {
			_, err := reg.AddPeer(info, overwrite)
			if err != nil {
				return fmt.Errorf("failed to add peer: %v", err)
			}
		}
	case peer.TypeUnregister:
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
