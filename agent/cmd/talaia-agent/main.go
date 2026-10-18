// Command talaia-agent is the Talaia per-server agent.
package main

import "fmt"

// version is the version placeholder, overridden at build time.
var version = "dev"

func main() {
	fmt.Printf("talaia-agent %s\n", version)
}
