# 原生桌面检查

`desktop,ci` 验证共用界面逻辑；这里使用真实 Fyne/OpenGL、X11、剪贴板和 D-Bus 托盘后端。测试程序使用临时配置与本地 loopback 服务，不捕获键鼠或音频，不启用局域网发现。只有显式设置 `SUPERFLOW_NATIVE_DESKTOP_TEST=1` 才能运行。请使用隔离桌面会话；`--clipboard` 会写入该会话的剪贴板。

## Linux 隔离图形环境

在仓库根目录构建测试镜像及 Go 检查程序，镜像架构跟随宿主机：

```console
docker build -f tests/desktop/Dockerfile -t superflow-desktop-test .
docker run --rm --entrypoint go -v "$PWD:/work" -w /work superflow-desktop-test build -tags desktop,smoke -o .build/superflow-native-check ./cmd/superflow-desktop-check
```

检查托盘注册、真实图标、Activate、面板重启、窗口关闭后继续共享、二次启动唤回、中文剪贴板及六页截图：

```console
docker run --rm --entrypoint sh -v "$PWD:/work" -w /work -e SUPERFLOW_NATIVE_DESKTOP_TEST=1 -e XDG_RUNTIME_DIR=/tmp/superflow-runtime superflow-desktop-test -c 'mkdir -p /tmp/superflow-runtime && chmod 700 /tmp/superflow-runtime && exec dbus-run-session -- xvfb-run -a -s "-screen 0 1280x960x24" sh -c "openbox >/tmp/openbox.log 2>&1 & exec /work/.build/superflow-native-check --out /work/.test-results/native-desktop/tray --tray=true --clipboard"'
```

同一命令换成 `--tray=false --background-start` 和另一个 `--out` 目录，验证没有托盘面板的后台启动与唤回。检查程序中的临时 StatusNotifierWatcher 仅在隔离会话运行，发现已有面板时会拒绝替换。

在 ARM 主机仿真 x64 时，如默认 Mesa llvmpipe 返回黑帧，可给容器添加 `-e GALLIUM_DRIVER=softpipe`。检查程序要求读回的每页画面同时包含浅色表面和深色文字，空白/黑帧会使检查失败；不能只凭窗口/托盘流程成功就判定渲染正常。

无会话总线的检查，在 Xvfb 启动后的内部命令中设置 `DBUS_SESSION_BUS_ADDRESS=unix:path=/tmp/no-session-bus`，省略外层 `dbus-run-session` 并使用 `--tray=false --background-start`。Fyne 可能记录无法监听系统主题的提示，但应用应继续运行并通过检查。

检查程序使用 40 秒上下文期限，成功输出 `native desktop check passed`。截图使用应用自己的 Canvas，机器可读结果为输出目录中的 `report.json`。ARM64 / x64 应分别构建并运行对应架构的程序；x64 容器在 ARM Mac 上使用 Docker 的 amd64 仿真，不能等同于实体 x64 桌面测试。

## 平台发行包

```console
go run ./cmd/superflow-build --desktop
go run ./cmd/superflow-build --desktop --os windows --arch amd64
go run ./cmd/superflow-build --desktop --os linux --arch arm64
```

后两条仍需对应的 C 编译器和图形开发库；Windows 交叉构建设置 `CC=x86_64-w64-mingw32-gcc`。可添加 `--opus-library PATH --opus-license PATH`；库必须与目标平台/架构匹配。Go 构建器生成图标、Windows COFF 资源、清单、版本、归档和依赖许可，Mac 还执行本地临时签名。

真实 Windows EXE 可在任何系统静态检查：

```console
SUPERFLOW_WINDOWS_BINARY="$PWD/dist/superflow-desktop/superflow-windows-amd64.exe" go test ./cmd/superflow-build -run 'TestWindowsResources|TestPackagedWindowsGUI' -v
```

这不替代 Windows 实机的托盘、DPI、窗口激活、快捷方式和注册表启动项测试。用户安装测试只在临时目录写文件；不为测试启用实际用户的登录启动。
