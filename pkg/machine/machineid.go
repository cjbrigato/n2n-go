package machine

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
)

const (
	uidFile = "machine-id"
)

var (
	appDirCache    string
	machineIdCache []byte
)

func filePath(f string) string {
	dir := appDir()
	return path.Join(dir, f)
}

func fileExists(f string) bool {
	fp := filePath(f)
	if _, err := os.Stat(fp); os.IsNotExist(err) {
		return false
	}
	return true
}

func appDir() string {
	if appDirCache == "" {
		s, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		appDirCache = path.Join(s, ".n2n-go")
	}
	return appDirCache
}

func ensureDirectory() {
	dir := appDir()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.Mkdir(dir, 0755)
	}
}

func ensureMachineID() {
	if !fileExists(uidFile) {
		ensureDirectory()
		id := make([]byte, 16)
		_, err := rand.Read(id)
		if err != nil {
			log.Fatalf("failed to generate random machine ID: %w", err)
		}
		err = os.WriteFile(filePath(uidFile), []byte(hex.EncodeToString(id)), 0755)
		if err != nil {
			log.Fatalf("cannot write machine-id file:%v", err)
		}
	}
}

func GetMachineID() ([]byte, error) {
	ensureMachineID()
	if machineIdCache != nil {
		return machineIdCache, nil // Return cached value if available
	}
	content, err := os.ReadFile(filePath(uidFile))
	if err == nil {
		idStr := strings.TrimSpace(string(content))
		if len(idStr) == 32 { // Check if it's a valid hex string (32 chars for 16 bytes)
			data, err := hex.DecodeString(idStr)
			if err != nil {
				return nil, fmt.Errorf("cannot decode machine-id")
			}
			if err == nil {
				machineIdCache = data
				return machineIdCache, nil
			}
		}
	}
	return nil, fmt.Errorf("unable to get machine id")
}
