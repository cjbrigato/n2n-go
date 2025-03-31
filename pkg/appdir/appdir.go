package appdir

import (
	"log"
	"os"
	"path"
)

var appDirCache string

func AppDir() string {
	if appDirCache == "" {
		s, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("%v", err)
		}
		appDirCache = path.Join(s, ".n2n-go")
	}
	return appDirCache
}

func ensureDirectory() {
	dir := AppDir()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.Mkdir(dir, 0755)
	}
}

func init() {
	ensureDirectory()
}
