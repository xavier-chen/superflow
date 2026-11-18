package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"
)

type dependencyModule struct {
	Path, Version, Dir string
	Main               bool
}

func dependencyNotices(environment []string, target string) ([]byte, error) {
	command := exec.Command("go", "list", "-deps", "-json", "-tags", "desktop", target)
	command.Env = environment
	data, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("读取桌面依赖信息：%w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	modules := map[string]dependencyModule{}
	for {
		var item struct{ Module *dependencyModule }
		err = decoder.Decode(&item)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if item.Module != nil && !item.Module.Main {
			modules[item.Module.Path] = *item.Module
		}
	}
	var out strings.Builder
	out.WriteString("SuperFlow includes the following third-party components and license texts.\n\n")
	if license, err := os.ReadFile(filepath.Join(runtime.GOROOT(), "LICENSE")); err == nil {
		out.WriteString("Go standard library and runtime\n\n")
		out.Write(license)
		out.WriteString("\n\n")
	}
	paths := make([]string, 0, len(modules))
	for path := range modules {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		module := modules[path]
		fmt.Fprintf(&out, "===== %s %s =====\n", module.Path, module.Version)
		if module.Dir == "" {
			continue
		}
		_ = filepath.WalkDir(module.Dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			relative, _ := filepath.Rel(module.Dir, path)
			if entry.IsDir() {
				name := entry.Name()
				if strings.Count(relative, string(filepath.Separator)) > 6 || name == "testdata" || name == "examples" || name == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			name := strings.ToLower(entry.Name())
			if !strings.Contains(name, "license") && !strings.HasPrefix(name, "copying") && !strings.HasPrefix(name, "notice") {
				return nil
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
				return nil
			}
			value, err := os.ReadFile(path)
			if err != nil || !utf8.Valid(value) {
				return nil
			}
			fmt.Fprintf(&out, "\n%s\n\n", filepath.ToSlash(relative))
			out.Write(value)
			out.WriteString("\n")
			return nil
		})
		out.WriteString("\n")
	}
	return []byte(out.String()), nil
}
