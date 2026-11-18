package instance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSecondProcessActivatesExistingProfile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	root := t.TempDir()
	profile := filepath.Join(root, "profile.json")
	primary, err := openAt(ctx, root, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInstanceHelper$")
	command.Env = append(os.Environ(), "SUPERFLOW_INSTANCE_TEST_ROOT="+root, "SUPERFLOW_INSTANCE_TEST_PROFILE="+profile)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	select {
	case <-primary.Activations:
	case <-ctx.Done():
		t.Fatal("existing window was not activated")
	}
	other, err := openAt(ctx, root, filepath.Join(root, "other.json"))
	if err != nil || other == nil {
		t.Fatal("different configuration blocked", err)
	}
	other.Close()
	primary.Close()
	replacement, err := openAt(ctx, root, profile)
	if err != nil || replacement == nil {
		t.Fatal("stale owner prevented restart", err)
	}
	replacement.Close()
}
func TestInstanceHelper(t *testing.T) {
	root := os.Getenv("SUPERFLOW_INSTANCE_TEST_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	result, err := openAt(ctx, root, os.Getenv("SUPERFLOW_INSTANCE_TEST_PROFILE"))
	if result != nil {
		defer result.Close()
	}
	if err != nil || result != nil {
		t.Fatal("second process started another instance", err)
	}
}
func TestActivationRejectsWrongTokenAndRemoteRecords(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	root := t.TempDir()
	owner, err := openAt(ctx, root, filepath.Join(root, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	r, err := readRecord(owner.recordPath)
	if err != nil {
		t.Fatal(err)
	}
	r.Token = strings.Repeat("0", 64)
	if err = activate(ctx, r); err == nil {
		t.Fatal("unauthenticated activation accepted")
	}
	select {
	case <-owner.Activations:
		t.Fatal("wrong token activated the window")
	default:
	}
	bad := filepath.Join(root, "bad.json")
	if err = os.WriteFile(bad, []byte(`{"Address":"192.168.1.2:42800","Token":"`+r.Token+`","PID":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readRecord(bad); err == nil {
		t.Fatal("activation record allowed remote networking")
	}
}
