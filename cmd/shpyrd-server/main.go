// The open-source platform server: the API, the controllers, the store,
// the extensions, one implicit workspace. Everything is in pkg/server; a
// binary adding the cloud layer imports it and passes its own Options.
package main

import "github.com/shpyrd-io/shpyrd/pkg/server"

func main() { server.Main(server.Options{Extensions: enterprise()}) }
