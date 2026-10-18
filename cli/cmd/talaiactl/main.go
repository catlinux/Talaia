// Command talaiactl is the Talaia command-line client.
package main

import "fmt"

// version is the version placeholder, overridden at build time.
var version = "dev"

func main() {
	fmt.Printf("talaiactl %s\n", version)
}
