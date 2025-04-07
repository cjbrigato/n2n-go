package main

import (
	"fmt"
	stdlog "log"
	"n2n-go/pkg/edge"
	"os"
)

func ctl(command string) {
	mgmt, err := edge.NewEdgeManagementClient()
	if err != nil {
		stdlog.Fatalf("failed to instanciate management client: %v", err)
	}
	res, err := mgmt.SendCommand(command)
	if err != nil {
		stdlog.Fatalf("%v", err)
	}
	fmt.Println(res)
	os.Exit(0)
}
