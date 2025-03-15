package tuntap

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

/*type Link struct {
	link netlink.Link
}*/

//func LinkFromInterface(iFace *Interface) (*Link, error) {

func (i *Interface) IfUp(addrStr string) error {

	link, err := netlink.LinkByName(i.Name())
	if err != nil {
		return fmt.Errorf("cannot get link %q", i.Name(), err)
	}
	addr, err := netlink.ParseAddr(addrStr)
	if err != nil {
		return fmt.Errorf("cannot get link %q", i.Name(), err)
	}
	err = netlink.AddrAdd(link, addr)
	if err != nil {
		return fmt.Errorf("cannot add addr %q", addrStr, err)
	}
	err = netlink.LinkSetUp(link)
	if err != nil {
		return fmt.Errorf("cannot bring up %q", i.Name(), err)
	}
	return nil
}
