//go:build !windows

package integration

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLaunchAgentIsValidXMLAndPreservesArguments(t *testing.T) {
	data, err := launchdManifest("/Applications/My & App/SuperFlow", "/tmp/profile <test>.json")
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var values []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "string" {
			var value string
			if err = decoder.DecodeElement(&value, &start); err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
	}
	joined := strings.Join(values, "|")
	if !strings.Contains(joined, "My & App") || !strings.Contains(joined, "--background|--config|/tmp/profile <test>.json") {
		t.Fatal("launchd argument escaping failed", joined)
	}
	if runtime.GOOS == "darwin" {
		path := filepath.Join(t.TempDir(), "app.plist")
		if err = os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("plutil", "-lint", path).CombinedOutput(); err != nil {
			t.Fatal(string(out), err)
		}
	}
}
