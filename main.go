package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/carlmjohnson/haystack/pinboard"
)

func main() {
	retCode := 0
	if err := pinboard.CLI(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			retCode = 2
		} else {
			retCode = 1
			fmt.Fprintf(os.Stderr, "Runtime error: %v\n", err)
		}
	}
	os.Exit(retCode)
}
