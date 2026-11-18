//go:build desktop && smoke

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"superflow.local/superflow/internal/desktop"
	"time"
)

func main() {
	out := flag.String("out", ".test-results/native-desktop", "screenshot and report directory")
	tray := flag.Bool("tray", true, "provide an isolated test StatusNotifierWatcher")
	clip := flag.Bool("clipboard", false, "exercise clipboard in an isolated graphical session")
	background := flag.Bool("background-start", false, "start hidden before activating through IPC")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := desktop.CheckNative(ctx, *out, *tray, *clip, *background); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("native desktop check passed")
}
