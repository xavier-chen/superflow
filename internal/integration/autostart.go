//go:build !windows

package integration

import (
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"runtime"
	"superflow.local/superflow/internal/brand"
)

func AutoStartEnabled() bool {
	target, err := currentLocations()
	if err != nil {
		return false
	}
	info, err := os.Stat(target.Autostart)
	return err == nil && info.Mode().IsRegular()
}
func SetAutoStart(enabled bool, profiles ...string) error {
	target, err := currentLocations()
	if err != nil {
		return err
	}
	if !enabled {
		err = os.Remove(target.Autostart)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	executable, err := Install(profiles...)
	if err != nil {
		return err
	}
	profile := ""
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	return writeAutostart(runtime.GOOS, executable, target, profile)
}
func writeAutostart(platform, executable string, target locations, profile string) error {
	if platform != "darwin" {
		entry, err := desktopEntry(executable, target.Icon, true, profile)
		if err != nil {
			return err
		}
		return writeAtomic(target.Autostart, []byte(entry), 0644)
	}
	data, err := launchdManifest(executable, profile)
	if err != nil {
		return err
	}
	return writeAtomic(target.Autostart, data, 0644)
}
func launchdManifest(executable, profile string) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteString(xml.Header)
	encoder := xml.NewEncoder(&buffer)
	plist := xml.StartElement{Name: xml.Name{Local: "plist"}, Attr: []xml.Attr{{Name: xml.Name{Local: "version"}, Value: "1.0"}}}
	dict := xml.StartElement{Name: xml.Name{Local: "dict"}}
	array := xml.StartElement{Name: xml.Name{Local: "array"}}
	_ = encoder.EncodeToken(plist)
	_ = encoder.EncodeToken(dict)
	element := func(name, value string) error {
		return encoder.EncodeElement(value, xml.StartElement{Name: xml.Name{Local: name}})
	}
	_ = element("key", "Label")
	_ = element("string", brand.ApplicationID)
	_ = element("key", "ProgramArguments")
	_ = encoder.EncodeToken(array)
	arguments := []string{executable, "ui", "--background"}
	if profile != "" {
		arguments = append(arguments, "--config", profile)
	}
	for _, argument := range arguments {
		if err := element("string", argument); err != nil {
			return nil, err
		}
	}
	_ = encoder.EncodeToken(array.End())
	_ = element("key", "RunAtLoad")
	_ = element("true", "")
	_ = encoder.EncodeToken(dict.End())
	_ = encoder.EncodeToken(plist.End())
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
