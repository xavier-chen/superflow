// Package instance keeps one desktop process per configuration and safely
// forwards a second launch to the existing window. IPC can only activate a
// window; it cannot change settings, send input or start sharing.
package instance

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type record struct {
	Address, Token string
	PID            int
}
type Instance struct {
	Activations <-chan struct{}
	events      chan struct{}
	lock        *os.File
	listener    net.Listener
	recordPath  string
	token       string
	once        sync.Once
	done        chan struct{}
}

// Open returns nil when another process has accepted the activation request.
func Open(ctx context.Context, configPath string) (*Instance, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return openAt(ctx, filepath.Join(root, "SuperFlow", "instances"), configPath)
}
func openAt(ctx context.Context, root, configPath string) (*Instance, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	path, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	if resolved, e := filepath.EvalSymlinks(path); e == nil {
		path = resolved
	}
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	key := sha256.Sum256([]byte(filepath.Clean(path)))
	name := hex.EncodeToString(key[:16])
	if err = os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("应用运行目录不可用")
	}
	if err = os.Chmod(root, 0700); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(root, name+".lock")
	if info, e := os.Lstat(lockPath); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("应用锁文件不可用")
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			lock.Close()
		}
	}()
	recordPath := filepath.Join(root, name+".json")
	deadline := time.Now().Add(3 * time.Second)
	for {
		owned, e := tryLock(lock)
		if e != nil {
			return nil, e
		}
		if owned {
			result, e := start(lock, recordPath)
			if e != nil {
				unlock(lock)
				return nil, e
			}
			keep = true
			go func() {
				select {
				case <-ctx.Done():
					result.Close()
				case <-result.done:
				}
			}()
			return result, nil
		}
		if r, e := readRecord(recordPath); e == nil {
			if e = activate(ctx, r); e == nil {
				return nil, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, errors.New("SuperFlow 正在启动，请稍后重新打开")
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
func start(lock *os.File, path string) (*Instance, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		listener.Close()
		return nil, err
	}
	events := make(chan struct{}, 1)
	i := &Instance{Activations: events, events: events, lock: lock, listener: listener, recordPath: path, token: hex.EncodeToString(secret), done: make(chan struct{})}
	data, _ := json.Marshal(record{Address: listener.Addr().String(), Token: i.token, PID: os.Getpid()})
	f, err := os.CreateTemp(filepath.Dir(path), ".instance-*")
	if err != nil {
		listener.Close()
		return nil, err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	// A stale record from a crashed instance contains no useful credential.
	if err == nil {
		err = os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	if err == nil {
		err = os.Rename(temp, path)
	}
	if err != nil {
		listener.Close()
		return nil, err
	}
	go i.serve()
	return i, nil
}
func readRecord(path string) (record, error) {
	var r record
	info, err := os.Lstat(path)
	if err != nil {
		return r, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1024 {
		return r, errors.New("invalid activation record")
	}
	file, err := os.Open(path)
	if err != nil {
		return r, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1025))
	if err != nil {
		return r, err
	}
	if json.Unmarshal(data, &r) != nil {
		return r, errors.New("invalid activation record")
	}
	host, port, err := net.SplitHostPort(r.Address)
	n, e := strconv.Atoi(port)
	token, t := hex.DecodeString(r.Token)
	if err != nil || e != nil || host != "127.0.0.1" || n < 1 || n > 65535 || t != nil || len(token) != 32 || r.PID <= 0 {
		return r, errors.New("invalid activation destination")
	}
	return r, nil
}
func activate(ctx context.Context, r record) error {
	dialer := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := dialer.DialContext(ctx, "tcp4", r.Address)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	allowForeground(r.PID)
	if _, err = io.WriteString(conn, "SUPERFLOW-ACTIVATE/1 "+r.Token+"\n"); err != nil {
		return err
	}
	response := make([]byte, 3)
	if _, err = io.ReadFull(conn, response); err != nil {
		return err
	}
	if string(response) != "OK\n" {
		return errors.New("activation rejected")
	}
	return nil
}
func (i *Instance) serve() {
	slots := make(chan struct{}, 4)
	for {
		conn, err := i.listener.Accept()
		if err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
		default:
			conn.Close()
			continue
		}
		go func() {
			defer func() { <-slots; conn.Close() }()
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			expected := "SUPERFLOW-ACTIVATE/1 " + i.token + "\n"
			data := make([]byte, len(expected))
			if _, err := io.ReadFull(conn, data); err != nil {
				return
			}
			if subtle.ConstantTimeCompare(data, []byte(expected)) != 1 {
				return
			}
			select {
			case i.events <- struct{}{}:
			default:
			}
			_, _ = io.WriteString(conn, "OK\n")
		}()
	}
}
func (i *Instance) Close() {
	i.once.Do(func() {
		_ = i.listener.Close()
		_ = os.Remove(i.recordPath)
		unlock(i.lock)
		_ = i.lock.Close()
		close(i.done)
	})
}
func (i *Instance) String() string {
	return fmt.Sprintf("SuperFlow desktop (%s)", filepath.Base(i.recordPath))
}
