package server

import "os"

func publicAPIListenAddr() string {
	if os.Getenv("SHPYRD_PUBLIC_API_ENABLED") == "true" {
		return ":8084"
	}
	return ""
}
