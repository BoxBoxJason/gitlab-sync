package utils

import (
	"log"
	"sync"
)

var (
	verbose   bool
	muVerbose = &sync.RWMutex{}
)

func SetVerbose(v bool) {
	muVerbose.Lock()
	defer muVerbose.Unlock()
	verbose = v
}

func getVerbose() bool {
	muVerbose.RLock()
	defer muVerbose.RUnlock()
	return verbose
}

func LogVerbose(message string) {
	if getVerbose() {
		log.Println(message)
	}
}

func LogVerbosef(format string, args ...any) {
	if getVerbose() {
		log.Printf(format, args...)
	}
}

func Log(message string) {
	log.Println(message)
}
