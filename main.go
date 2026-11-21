// Package main is the entry point of the gitlab-sync binary.
package main

import (
	"os"

	appcmd "github.com/boxboxjason/gitlab-sync/cmd"
)

func main() {
	os.Exit(appcmd.Execute())
}
