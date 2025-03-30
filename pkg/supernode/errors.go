package supernode

import "errors"

var (
	ErrCommunityUnknownEdge = errors.New("found no registered edge")
	ErrCommunityNotFound    = errors.New("no community found with this hash")
	ErrUnicastForwardFail   = errors.New("cannot find edge for unicast forwarding")
)
