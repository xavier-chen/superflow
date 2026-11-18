// Package config owns the v2 configuration. It never reads or modifies v1 keys.
package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Audio struct {
	Codec          string   `json:"codec"`
	SendSystem     []string `json:"send_system,omitempty"`
	SendMicrophone []string `json:"send_microphone,omitempty"`
	PlaySystem     []string `json:"play_system,omitempty"`
	PlayMicrophone []string `json:"play_microphone,omitempty"`
	// Commands exchange 48kHz stereo signed 16-bit little endian PCM on stdin/stdout.
	// Each element is one argv entry. No shell is involved.
	Target   string             `json:"target,omitempty"`
	BufferMS int                `json:"buffer_ms"`
	Gains    map[string]float64 `json:"gains,omitempty"`
}

type Screen struct {
	Name string `json:"name"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
}

type Config struct {
	Version          int      `json:"version"`
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Mode             string   `json:"mode"`
	Listen           string   `json:"listen"`
	Server           string   `json:"server,omitempty"`
	ServerAlternates []string `json:"server_alternates,omitempty"`
	Secret           string   `json:"secret"`
	Pin              string   `json:"server_certificate_sha256,omitempty"`
	Certificate      string   `json:"certificate_pem,omitempty"`
	PrivateKey       string   `json:"private_key_pem,omitempty"`
	Input            bool     `json:"input"`
	InputDevices     []string `json:"input_devices,omitempty"`
	ScreenWidth      int      `json:"screen_width,omitempty"`
	ScreenHeight     int      `json:"screen_height,omitempty"`
	Clipboard        bool     `json:"clipboard"`
	AutoLayout       bool     `json:"auto_layout,omitempty"`
	Screens          []Screen `json:"screens"`
	Audio            Audio    `json:"audio"`
}

func DefaultPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "SuperFlow", "v2.json")
}

func New(mode, name string) (*Config, error) {
	if name == "" {
		name, _ = os.Hostname()
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	s := make([]byte, 32)
	if _, err := rand.Read(s); err != nil {
		return nil, err
	}
	c := &Config{Version: 2, ID: hex.EncodeToString(b), Name: name, Mode: mode,
		Listen: ":42800", Secret: base64.RawURLEncoding.EncodeToString(s), Input: true,
		Audio: Audio{BufferMS: 40, Codec: "opus"}, Screens: []Screen{{Name: name}}}
	if mode == "hub" {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
		if err != nil {
			return nil, err
		}
		t := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "SuperFlow v2"},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(5, 0, 0),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		der, err := x509.CreateCertificate(rand.Reader, t, t, &key.PublicKey, key)
		if err != nil {
			return nil, err
		}
		pk, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, err
		}
		c.Certificate = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
		c.PrivateKey = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}))
		p := sha256.Sum256(der)
		c.Pin = hex.EncodeToString(p[:])
	}
	return c, c.Validate()
}

func (c *Config) Key() ([]byte, error) { return base64.RawURLEncoding.DecodeString(c.Secret) }

func (c *Config) Code() (string, error) {
	key, err := c.Key()
	if err != nil || len(key) != 32 {
		return "", errors.New("invalid secret")
	}
	pin, err := hex.DecodeString(c.Pin)
	if err != nil || len(pin) != 32 {
		return "", errors.New("invalid certificate pin")
	}
	return "SF2-" + base64.RawURLEncoding.EncodeToString(append(key, pin...)), nil
}

func (c *Config) Pair(code, server string) error {
	if !strings.HasPrefix(code, "SF2-") {
		return errors.New("Go 版配对码必须以 SF2- 开头，不能使用旧版 SF1 密钥")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, "SF2-"))
	if err != nil || len(b) != 64 {
		return errors.New("invalid pairing code")
	}
	c.Mode = "node"
	c.Server = server
	c.Secret = base64.RawURLEncoding.EncodeToString(b[:32])
	c.Pin = hex.EncodeToString(b[32:])
	return c.Validate()
}

func (c *Config) Validate() error {
	if c.Version != 2 {
		return errors.New("unsupported config version")
	}
	id, err := hex.DecodeString(c.ID)
	if err != nil || len(id) != 16 {
		return errors.New("invalid device ID")
	}
	if !ValidName(c.Name) {
		return errors.New("设备名称不能为空或过长，请修改名称后重试")
	}
	key, err := c.Key()
	if err != nil || len(key) != 32 {
		return errors.New("invalid secret")
	}
	if c.Mode != "hub" && c.Mode != "node" {
		return errors.New("mode must be hub or node")
	}
	if c.Mode == "hub" {
		if _, _, err := net.SplitHostPort(c.Listen); err != nil {
			return fmt.Errorf("listen: %w", err)
		}
		if c.Certificate == "" || c.PrivateKey == "" {
			return errors.New("hub certificate missing")
		}
	} else if c.Server != "" {
		if _, _, err := net.SplitHostPort(c.Server); err != nil {
			return fmt.Errorf("server: %w", err)
		}
		pin, err := hex.DecodeString(c.Pin)
		if err != nil || len(pin) != 32 {
			return errors.New("server certificate pin missing")
		}
	}
	if c.Audio.BufferMS < 10 || c.Audio.BufferMS > 200 {
		return errors.New("audio buffer_ms must be 10–200")
	}
	if len(c.ServerAlternates) > 15 {
		return errors.New("备用连接地址过多")
	}
	for _, address := range c.ServerAlternates {
		if !ValidAddress(address) {
			return errors.New("备用连接地址无效")
		}
	}
	if c.Audio.Codec != "pcm" && c.Audio.Codec != "opus" && c.Audio.Codec != "auto" {
		return errors.New("audio codec must be auto, pcm or opus")
	}
	if err := validateGains(c.Audio.Gains); err != nil {
		return err
	}
	if c.Mode == "hub" && len(c.Audio.SendMicrophone) > 0 && c.Audio.Target == "" {
		return errors.New("请先为麦克风选择一台接收设备")
	}
	seen := map[string]bool{}
	positions := map[[2]int]bool{}
	for _, s := range c.Screens {
		p := [2]int{s.X, s.Y}
		if !ValidName(s.Name) || seen[s.Name] || positions[p] {
			return errors.New("设备名称和屏幕位置不能重复")
		}
		seen[s.Name] = true
		positions[p] = true
	}
	if !seen[c.Name] {
		return errors.New("screens must include the local device name")
	}
	return nil
}
func ValidName(name string) bool {
	if strings.TrimSpace(name) == "" || len(name) > 80 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var c Config
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return nil, err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return nil, errors.New("trailing config data")
	}
	return &c, c.Validate()
}

// Save refuses to replace an existing file; a failed init cannot destroy pairing.
func Save(path string, c *Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".superflow-new-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Publish a completely written file without ever replacing another identity.
	return os.Link(temp, path)
}
