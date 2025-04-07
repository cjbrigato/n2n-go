package log

import (
	"fmt"
	stdlog "log"
)

func MustInit(app string) {
	err := Init(fmt.Sprintf("%s.db", app))
	if err != nil {
		stdlog.Fatalf("FATAL: Failed to initialize logger: %v\n", err)
	}
}
