package main

import (
	"bytes"
	"debug/pe"
	"os"
	"path/filepath"
	"strings"

	"github.com/tc-hib/winres"
	resourceversion "github.com/tc-hib/winres/version"
	"superflow.local/superflow/internal/brand"
	"superflow.local/superflow/internal/version"
)

func windowsResources(arch string) ([]byte, error) {
	var resources winres.ResourceSet
	data, err := brand.ICO()
	if err != nil {
		return nil, err
	}
	icon, err := winres.LoadICO(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if err = resources.SetIcon(winres.ID(1), icon); err != nil {
		return nil, err
	}
	resources.SetManifest(winres.AppManifest{
		Identity:    winres.AssemblyIdentity{Name: brand.ApplicationID, Version: version.Numeric},
		Description: "SuperFlow", Compatibility: winres.Win10AndAbove, ExecutionLevel: winres.AsInvoker,
		DPIAwareness: winres.DPIPerMonitorV2, LongPathAware: true, UseCommonControlsV6: true,
		HighResolutionScrollingAware: true, UltraHighResolutionScrollingAware: true,
	})
	info := resourceversion.Info{FileVersion: version.Numeric, ProductVersion: version.Numeric, Type: resourceversion.App}
	info.Flags.Prerelease = true
	info.Set(resourceversion.LangDefault, resourceversion.ProductName, "SuperFlow")
	info.Set(resourceversion.LangDefault, resourceversion.FileDescription, "SuperFlow connected workspace")
	info.Set(resourceversion.LangDefault, resourceversion.FileVersion, version.Release)
	info.Set(resourceversion.LangDefault, resourceversion.ProductVersion, version.Release)
	info.Set(resourceversion.LangDefault, resourceversion.OriginalFilename, "SuperFlow.exe")
	resources.SetVersionInfo(info)
	var out bytes.Buffer
	err = resources.WriteObject(&out, winres.Arch(arch))
	return out.Bytes(), err
}
func windowsSource(arch string) (string, func(), error) {
	if err := os.MkdirAll(".build", 0755); err != nil {
		return "", nil, err
	}
	stage, err := os.MkdirTemp(".build", "desktop-windows-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(stage) }
	entries, err := os.ReadDir("cmd/superflow")
	if err != nil {
		cleanup()
		return "", nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			if err = copyFile(filepath.Join("cmd/superflow", entry.Name()), filepath.Join(stage, entry.Name()), 0644); err != nil {
				cleanup()
				return "", nil, err
			}
		}
	}
	resources, err := windowsResources(arch)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if err = os.WriteFile(filepath.Join(stage, "resources_windows_"+arch+".syso"), resources, 0644); err != nil {
		cleanup()
		return "", nil, err
	}
	return "./" + filepath.ToSlash(stage), cleanup, nil
}
func verifyWindowsGUI(path, arch string) error {
	file, err := pe.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	wanted := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
	if arch == "arm64" {
		wanted = pe.IMAGE_FILE_MACHINE_ARM64
	}
	if file.Machine != wanted {
		return &resourceError{"wrong Windows architecture"}
	}
	header, ok := file.OptionalHeader.(*pe.OptionalHeader64)
	if !ok || header.Subsystem != pe.IMAGE_SUBSYSTEM_WINDOWS_GUI {
		return &resourceError{"a console executable was packaged as GUI"}
	}
	stream, err := os.Open(path)
	if err != nil {
		return err
	}
	defer stream.Close()
	resources, err := winres.LoadFromEXE(stream)
	if err != nil {
		return err
	}
	manifest, icon, version := false, false, false
	resources.Walk(func(kind, id winres.Identifier, language uint16, data []byte) bool {
		if kind == winres.RT_MANIFEST {
			value := string(data)
			value = strings.ToLower(value)
			manifest = strings.Contains(value, "permonitorv2") && strings.Contains(value, "asinvoker")
		}
		if kind == winres.RT_GROUP_ICON {
			icon = true
		}
		if kind == winres.RT_VERSION {
			version = true
		}
		return true
	})
	if !manifest || !icon || !version {
		return &resourceError{"incomplete native Windows resources"}
	}
	return nil
}

type resourceError struct{ message string }

func (e *resourceError) Error() string { return e.message }
