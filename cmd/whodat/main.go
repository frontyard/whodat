// whodat — unofficial WhatsApp messaging app for Linux.
//
// Qt6/QML UI (miqt); WhatsApp via whatsmeow.
package main

import "os"

// version is set via -ldflags "-X main.version=…"
var version = "dev"

func main() {
	os.Exit(runUI(version))
}
