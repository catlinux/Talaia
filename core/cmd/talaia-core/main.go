// Command talaia-core is the Talaia control plane server.
package main

import "fmt"

// version is the version placeholder, overridden at build time.
var version = "dev"

func main() {
	fmt.Printf("talaia-core %s\n", version)
}
