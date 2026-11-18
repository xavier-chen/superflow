package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sync"
)

var ErrChanged = errors.New("配置已被其他操作修改，请重新载入后保存")
var updateMu sync.Mutex

// MergeEdit keeps background updates (such as newly paired screens or gain)
// when a user saves an unrelated form. Concurrent edits to the same field fail.
func MergeEdit(base, edit, current *Config) (*Config, error) {
	if base == nil || edit == nil || current == nil || base.ID != current.ID {
		return nil, ErrChanged
	}
	result := current.Clone()
	source := base.Clone()
	changed := edit.Clone()
	var merge func(reflect.Value, reflect.Value, reflect.Value) error
	merge = func(a, b, d reflect.Value) error {
		for i := 0; i < a.NumField(); i++ {
			x, y, z := a.Field(i), b.Field(i), d.Field(i)
			if x.Kind() == reflect.Struct {
				if err := merge(x, y, z); err != nil {
					return err
				}
				continue
			}
			if reflect.DeepEqual(x.Interface(), y.Interface()) {
				continue
			}
			if !reflect.DeepEqual(x.Interface(), z.Interface()) && !reflect.DeepEqual(y.Interface(), z.Interface()) {
				return ErrChanged
			}
			z.Set(y)
		}
		return nil
	}
	if err := merge(reflect.ValueOf(source).Elem(), reflect.ValueOf(changed).Elem(), reflect.ValueOf(result).Elem()); err != nil {
		return nil, err
	}
	return result, result.Validate()
}

func LegacyPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "SoundFlow", "v2.json")
}

// LoadCompatible copies the existing Go v2 identity to the new product directory.
// Explicit custom paths never fall back to another file. The legacy file is retained.
func LoadCompatible(path string) (*Config, error) {
	c, err := Load(path)
	if err == nil || !errors.Is(err, os.ErrNotExist) || filepath.Clean(path) != filepath.Clean(DefaultPath()) {
		return c, err
	}
	return migrate(LegacyPath(), path)
}
func migrate(oldPath, newPath string) (*Config, error) {
	c, err := Load(oldPath)
	if err != nil {
		return nil, err
	}
	if err = Save(newPath, c); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Load(newPath)
		}
		return nil, fmt.Errorf("迁移配置: %w", err)
	}
	return c, nil
}
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	copy := *c
	copy.InputDevices = append([]string(nil), c.InputDevices...)
	copy.ServerAlternates = append([]string(nil), c.ServerAlternates...)
	copy.Screens = append([]Screen(nil), c.Screens...)
	copy.Audio.SendSystem = append([]string(nil), c.Audio.SendSystem...)
	copy.Audio.SendMicrophone = append([]string(nil), c.Audio.SendMicrophone...)
	copy.Audio.PlaySystem = append([]string(nil), c.Audio.PlaySystem...)
	copy.Audio.PlayMicrophone = append([]string(nil), c.Audio.PlayMicrophone...)
	if c.Audio.Gains != nil {
		copy.Audio.Gains = map[string]float64{}
		for k, v := range c.Audio.Gains {
			copy.Audio.Gains[k] = v
		}
	}
	return &copy
}
func validateGains(gains map[string]float64) error {
	if len(gains) > 64 {
		return errors.New("最多保留 64 台设备的音量设置")
	}
	for name, gain := range gains {
		if !ValidName(name) || math.IsNaN(gain) || math.IsInf(gain, 0) || gain < 0 || gain > 2 {
			return errors.New("设备音量必须在 0–200% 之间")
		}
	}
	return nil
}

// Replace atomically saves explicit settings edits. expected prevents losing changes
// from another in-process operation or an editor observed before the commit.
func Replace(path string, expected, next *Config) error {
	if expected == nil || next == nil {
		return errors.New("missing configuration")
	}
	if err := next.Validate(); err != nil {
		return err
	}
	updateMu.Lock()
	defer updateMu.Unlock()
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("请使用配置文件的真实路径保存设置")
	}
	current, err := Load(path)
	if err != nil {
		return err
	}
	a, _ := json.Marshal(current)
	b, _ := json.Marshal(expected)
	if !bytes.Equal(a, b) {
		return ErrChanged
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".superflow-config-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
