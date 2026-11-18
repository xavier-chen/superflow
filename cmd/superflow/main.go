package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"superflow.local/superflow/internal/app"
	"superflow.local/superflow/internal/audio"
	"superflow.local/superflow/internal/clipboard"
	"superflow.local/superflow/internal/config"
	"superflow.local/superflow/internal/desktop"
	"superflow.local/superflow/internal/integration"
)

type names []string

func (n *names) String() string     { return strings.Join(*n, ",") }
func (n *names) Set(v string) error { *n = append(*n, v); return nil }

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := execute(ctx, os.Args[1:], os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		fmt.Fprintln(os.Stderr, "SuperFlow:", err)
		if desktop.Available() && (len(os.Args) == 1 || os.Args[1] == "ui" || os.Args[1] == "install") {
			desktop.ReportStartupError(err)
		}
		os.Exit(1)
	}
}
func execute(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		if desktop.Available() {
			return desktop.Run(ctx, config.DefaultPath())
		}
		return usage(out)
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	path := fs.String("config", config.DefaultPath(), "v2 configuration path")
	switch args[0] {
	case "ui":
		background := fs.Bool("background", false, "start in the background after setup")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("unexpected arguments")
		}
		return desktop.Run(ctx, *path, desktop.Options{Background: *background})
	case "install":
		if !desktop.Available() {
			return errors.New("请使用桌面版本安装到本机")
		}
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		destination, err := integration.Install(*path)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "SuperFlow 已安装：", destination)
		return nil
	case "init":
		mode := fs.String("mode", "hub", "hub or node")
		name := fs.String("name", "", "unique device name")
		listen := fs.String("listen", ":42800", "hub UDP listen address")
		var peers names
		fs.Var(&peers, "peer", "remote screen name, repeat for a horizontal layout")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("unexpected arguments")
		}
		c, err := config.New(*mode, *name)
		if err != nil {
			return err
		}
		c.Listen = *listen
		for i, p := range peers {
			c.Screens = append(c.Screens, config.Screen{Name: p, X: i + 1})
		}
		if err = config.Save(*path, c); err != nil {
			return err
		}
		fmt.Fprintln(out, "配置已创建:", *path)
		if c.Mode == "hub" {
			code, err := c.Code()
			if err != nil {
				return err
			}
			fmt.Fprintln(out, "配对码（仅发给信任的设备）:", code)
		}
		return nil
	case "pair":
		server := fs.String("server", "", "hub host:port")
		name := fs.String("name", "", "unique device name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *server == "" {
			return errors.New("pair requires --server host:42800")
		}
		c, err := config.New("node", *name)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "粘贴 SF2 配对码并回车（不要使用旧版 SF1 密钥）:")
		line, err := bufio.NewReader(io.LimitReader(in, 256)).ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		if err = c.Pair(strings.TrimSpace(line), *server); err != nil {
			return err
		}
		if err = config.Save(*path, c); err != nil {
			return err
		}
		fmt.Fprintln(out, "配对配置已保存:", *path)
		return nil
	case "code":
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		c, err := config.LoadCompatible(*path)
		if err != nil {
			return err
		}
		if c.Mode != "hub" {
			return errors.New("code is only available on the hub")
		}
		code, err := c.Code()
		if err == nil {
			fmt.Fprintln(out, code)
		}
		return err
	case "run":
		noInput := fs.Bool("no-input", false, "disable input for this run")
		noClip := fs.Bool("no-clipboard", false, "disable clipboard for this run")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		c, err := config.LoadCompatible(*path)
		if err != nil {
			return err
		}
		if *noInput {
			c.Input = false
		}
		if *noClip {
			c.Clipboard = false
		}
		return app.Run(ctx, c)
	case "doctor":
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		c, err := config.LoadCompatible(*path)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s\nOS: %s/%s\n配置: %s\n角色: %s; 设备: %s\n键鼠: %t; 文本剪贴板: %t\n", app.Version(), runtime.GOOS, runtime.GOARCH, *path, c.Mode, c.Name, c.Input, c.Clipboard)
		if err = audio.OpusAvailable(); err != nil {
			fmt.Fprintln(out, "Opus:", err)
		} else {
			fmt.Fprintln(out, "Opus: available")
		}
		if c.Clipboard {
			if _, err = clipboard.New(); err != nil {
				fmt.Fprintln(out, "剪贴板依赖:", err)
			}
		}
		fmt.Fprintln(out, "诊断不会打开麦克风、抓取键盘或修改系统音频路由。使用 audio-devices 单独枚举音频设备。")
		return nil
	case "audio-devices":
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		ds, err := audio.Devices()
		if err != nil {
			return err
		}
		e := json.NewEncoder(out)
		e.SetIndent("", "  ")
		return e.Encode(ds)
	case "audio-capture":
		id := fs.String("device", "", "capture device ID from audio-devices")
		loop := fs.Bool("loopback", false, "Windows WASAPI system output capture")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return audio.NativeCapture(ctx, out, *id, *loop)
	case "audio-play":
		id := fs.String("device", "", "playback device ID from audio-devices")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return audio.NativePlayback(ctx, in, *id)
	case "version":
		fmt.Fprintln(out, app.Version())
		return nil
	case "help", "--help", "-h":
		return usage(out)
	default:
		return fmt.Errorf("unknown command %q; run superflow help", args[0])
	}
}
func usage(w io.Writer) error {
	_, err := fmt.Fprintln(w, `SuperFlow Go — 音频、麦克风、键鼠、文本剪贴板
  superflow init --name mac --peer linux --peer windows
  superflow ui [--config PATH] [--background]     # 托盘与桌面窗口
  superflow install [--config PATH]              # 安装桌面版到当前用户
  superflow pair --name linux --server 192.168.1.10:42800
  superflow run [--config PATH] [--no-input] [--no-clipboard]
  superflow code | doctor | audio-devices | version
  superflow audio-capture [--device ID] [--loopback]   # stdout: raw PCM
  superflow audio-play [--device ID]                 # stdin: raw PCM
快捷键: Ctrl+Alt+F12 切换首台远端/返回本机；Ctrl+Alt+Esc 返回本机。
命令行配置默认关闭声音与剪贴板；桌面版通过连接向导和共享开关配置。见 docs/GO.md。`)
	return err
}
