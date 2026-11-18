package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"superflow.local/superflow/internal/config"
)

func TestCLIInitPairDoctor(t *testing.T) {
	ctx := context.Background()
	hub := filepath.Join(t.TempDir(), "hub.json")
	node := filepath.Join(t.TempDir(), "node.json")
	var out bytes.Buffer
	if err := execute(ctx, []string{"init", "--config", hub, "--name", "mac", "--peer", "linux", "--listen", "127.0.0.1:0"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	h, err := config.Load(hub)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := h.Code()
	out.Reset()
	if err = execute(ctx, []string{"pair", "--config", node, "--name", "linux", "--server", "127.0.0.1:42800"}, strings.NewReader(code+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	n, err := config.Load(node)
	if err != nil || n.Pin != h.Pin || n.Secret != h.Secret {
		t.Fatal("CLI pairing lost server authentication", err)
	}
	out.Reset()
	if err = execute(ctx, []string{"doctor", "--config", node}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), n.Secret) || strings.Contains(out.String(), code) {
		t.Fatal("doctor leaked pairing material")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if err = execute(ctx, []string{"run", "--config", hub, "--no-input", "--no-clipboard"}, nil, &out); err != nil {
		t.Fatal("headless cancelled run", err)
	}
}
