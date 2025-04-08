package main

import (
	"fmt"
	stdlog "log"
	"n2n-go/pkg/edge"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
)

var (
	// Define the 'logs' subcommand
	ctlCommand = &cli.Command{
		Name:               "ctl",
		Usage:              "controls edge via management socket",
		UsageText:          "ctl [args...]",
		Description:        `controls edge via management socket`,
		CustomHelpTemplate: logsCommandHelpTemplate,
		Flags:              []cli.Flag{
			// --- Common Options ---

		},
		Action: ctlCmd,
	}
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

func ctlCmd(c *cli.Context) error {
	s := strings.Join(c.Args().Slice(), " ")
	fmt.Println(s)
	ctl(s)
	return nil
}
