package main

import "os"

func envDebugEnabled() bool {
	switch os.Getenv("GCLPR_DEBUG") {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	default:
		return false
	}
}
