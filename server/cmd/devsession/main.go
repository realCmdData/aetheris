// Command devsession prints a browser session token for local UI development, so the web UI can be
// exercised without a passkey. It opens the same -state dir as `aetheris serve -dev` and is never deployed.
// Use: document.cookie = "__Host-aetheris=<token>; Path=/; Secure" on http://localhost.
package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"aetheris/server/internal/auth"
)

func main() {
	state := flag.String("state", "", "the dev server's -state dir")
	flag.Parse()
	a, err := auth.Open(*state, "localhost", []string{"http://localhost:5173"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer a.Close()
	token, err := a.NewWebSession("Dev browser")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(token)
}
