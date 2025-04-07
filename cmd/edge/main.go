// cmd/edge/main.go (Modified)
package main

import (
	"fmt"
	"os"

	_ "github.com/cjbrigato/ensure/linux" // edge only works on linux right now
	"github.com/urfave/cli/v2"
)

const banner = "ICAgICBfICAgICAgIF8KICAgIC8gL19fIF9ffCB8X18gXyAgX18gICAKIF8gLyAvIC1fKSBfYCAvIF9gIC8gLV8pICAKKF8pXy9cX19fXF9fLF9cX18sIFxfX198Ci0tLS0tLS0tLS0tLS0tfF9fXy9AbjJuLWdvLSVzIChidWlsdCAlcykgICAgICAK"

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	// Define the main application
	app := &cli.App{
		Name:  "edge",
		Usage: "n2n-go edge",
		Commands: []*cli.Command{
			logsCommand,
			upCommand,
		},
	}

	// Run the application
	err := app.Run(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Application Error: %v\n", err)
		os.Exit(1)
	}
}

/*
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "ctl":
			ctl(strings.Join(os.Args[2:], " "))
		case "logs":
			s := ""
			if len(os.Args) > 2 {
				s = os.Args[2]
			}
			logs(s)
		case "up":
			up()
		}
	}
	fmt.Printf("need ctl/logs/up args")
}
*/
