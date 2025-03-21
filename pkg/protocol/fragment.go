package protocol

import (
	"fmt"
	"sync"
)

type Fragment struct {
	frags map[uint8][]byte
	h     ProtoVFragHeader
}

func (f *Fragment) set(data []byte, index uint8) (bool, error) {
	if index > (f.h.FragmentTotal - 1) {
		return false, fmt.Errorf("Fragment: out of bound write")
	}
	f.frags[index] = data
	return (len(f.frags) >= int(f.h.FragmentTotal)), nil
}

type FragmentBuffer struct {
	fragMu sync.RWMutex
	Fragments map[string]*Fragment
}


