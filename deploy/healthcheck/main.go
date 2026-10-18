// healthcheck asks the API in this container for GET /healthz and exits 0 on 200, 1 otherwise.
// The runtime image is distroless (no shell, no curl), so Docker's HEALTHCHECK runs this static binary.
//
// Port: $PORT (default 8080). Optional argument: a full URL to check instead.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	url := "http://127.0.0.1:" + env("PORT", "8080") + "/healthz"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		os.Exit(1)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck:", url, "answered", resp.Status)
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
