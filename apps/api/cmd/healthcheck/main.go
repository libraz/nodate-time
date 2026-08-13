// Command healthcheck is a minimal HTTP probe for the API container.
//
// The runtime image is distroless: it has no shell, curl or wget for a
// container health check to invoke, so the probe ships as a binary beside the
// server. It exits 0 when GET /health answers 2xx and non-zero otherwise.
//
// It reads the same TC_PORT the server binds, so a deployment that moves the
// port cannot end up probing the old one.
package main

import (
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("TC_PORT")
	if port == "" {
		port = "8080"
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/health")
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		os.Exit(0)
	}
	os.Exit(1)
}
