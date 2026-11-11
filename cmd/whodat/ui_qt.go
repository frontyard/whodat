package main

import "github.com/frontyard/whodat/internal/qtui"

func runUI(version string) int {
	qtui.SetVersion(version)
	return qtui.Run()
}
