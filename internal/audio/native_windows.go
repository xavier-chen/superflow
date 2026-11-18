//go:build windows

package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
	"golang.org/x/sys/windows"
)

func enumerator() (*wca.IMMDeviceEnumerator, error) {
	var e *wca.IMMDeviceEnumerator
	err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &e)
	return e, err
}
func endpoint(e *wca.IMMDeviceEnumerator, id string, flow uint32) (*wca.IMMDevice, error) {
	var device *wca.IMMDevice
	if id == "" {
		err := e.GetDefaultAudioEndpoint(flow, wca.EConsole, &device)
		return device, err
	}
	var list *wca.IMMDeviceCollection
	if err := e.EnumAudioEndpoints(flow, wca.DEVICE_STATE_ACTIVE, &list); err != nil {
		return nil, err
	}
	defer list.Release()
	var count uint32
	if err := list.GetCount(&count); err != nil {
		return nil, err
	}
	for i := uint32(0); i < count; i++ {
		if err := list.Item(i, &device); err != nil {
			return nil, err
		}
		var name string
		err := device.GetId(&name)
		if err == nil && name == id {
			return device, nil
		}
		device.Release()
	}
	return nil, errors.New("audio device is no longer available")
}
func Devices() ([]Device, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		return nil, err
	}
	defer ole.CoUninitialize()
	e, err := enumerator()
	if err != nil {
		return nil, err
	}
	defer e.Release()
	var out []Device
	for _, flow := range []uint32{wca.ERender, wca.ECapture} {
		def, _ := endpoint(e, "", flow)
		defaultID := ""
		if def != nil {
			_ = def.GetId(&defaultID)
			def.Release()
		}
		var list *wca.IMMDeviceCollection
		if err = e.EnumAudioEndpoints(flow, wca.DEVICE_STATE_ACTIVE, &list); err != nil {
			return nil, err
		}
		var count uint32
		_ = list.GetCount(&count)
		for i := uint32(0); i < count; i++ {
			var d *wca.IMMDevice
			if err = list.Item(i, &d); err != nil {
				list.Release()
				return nil, err
			}
			var id string
			_ = d.GetId(&id)
			name := id
			var props *wca.IPropertyStore
			if d.OpenPropertyStore(wca.STGM_READ, &props) == nil {
				var v wca.PROPVARIANT
				if props.GetValue(&wca.PKEY_Device_FriendlyName, &v) == nil {
					name = v.String()
					clear := windows.NewLazySystemDLL("ole32.dll").NewProc("PropVariantClear")
					_, _, _ = clear.Call(uintptr(unsafe.Pointer(&v)))
				}
				props.Release()
			}
			kind := "capture"
			if flow == wca.ERender {
				kind = "playback"
			}
			out = append(out, Device{kind, id, name, id == defaultID})
			d.Release()
		}
		list.Release()
	}
	return out, nil
}
func NativeCapture(ctx context.Context, w io.Writer, id string, loopback bool) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		return err
	}
	defer ole.CoUninitialize()
	e, err := enumerator()
	if err != nil {
		return err
	}
	defer e.Release()
	flow := uint32(wca.ECapture)
	flags := uint32(0)
	if loopback {
		flow = wca.ERender
		flags = wca.AUDCLNT_STREAMFLAGS_LOOPBACK
	}
	d, err := endpoint(e, id, flow)
	if err != nil {
		return err
	}
	defer d.Release()
	var client *wca.IAudioClient
	if err = d.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &client); err != nil {
		return err
	}
	defer client.Release()
	var format *wca.WAVEFORMATEX
	if err = client.GetMixFormat(&format); err != nil {
		return err
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(format)))
	tag := format.WFormatTag
	if tag == 0xfffe && format.CbSize >= 22 {
		tag = *(*uint16)(unsafe.Add(unsafe.Pointer(format), 24))
	}
	if tag != 1 && tag != 3 {
		return fmt.Errorf("unsupported WASAPI format tag: %d", tag)
	}
	convert := Converter{Rate: int(format.NSamplesPerSec), Channels: int(format.NChannels), Bits: int(format.WBitsPerSample), Float: tag == 3}
	if err = client.Initialize(wca.AUDCLNT_SHAREMODE_SHARED, flags, 200000, 0, format, nil); err != nil {
		return err
	}
	var capture *wca.IAudioCaptureClient
	if err = client.GetService(wca.IID_IAudioCaptureClient, &capture); err != nil {
		return err
	}
	defer capture.Release()
	if err = client.Start(); err != nil {
		return err
	}
	defer client.Stop()
	clock := time.NewTicker(3 * time.Millisecond)
	defer clock.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-clock.C:
			for {
				var size uint32
				if err = capture.GetNextPacketSize(&size); err != nil {
					return err
				}
				if size == 0 {
					break
				}
				var data *byte
				var frames, flags uint32
				if err = capture.GetBuffer(&data, &frames, &flags, nil, nil); err != nil {
					return err
				}
				var b []byte
				silent := flags&wca.AUDCLNT_BUFFERFLAGS_SILENT != 0
				if !silent && data != nil {
					b = unsafe.Slice(data, int(frames)*int(format.NBlockAlign))
				}
				pcm, convErr := convert.Convert(b, int(frames), silent)
				releaseErr := capture.ReleaseBuffer(frames)
				if convErr != nil {
					return convErr
				}
				if releaseErr != nil {
					return releaseErr
				}
				if len(pcm) > 0 {
					if _, err = w.Write(pcm); err != nil {
						return err
					}
				}
			}
		}
	}
}
func NativePlayback(ctx context.Context, r io.Reader, id string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		return err
	}
	defer ole.CoUninitialize()
	e, err := enumerator()
	if err != nil {
		return err
	}
	defer e.Release()
	d, err := endpoint(e, id, wca.ERender)
	if err != nil {
		return err
	}
	defer d.Release()
	var client *wca.IAudioClient
	if err = d.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &client); err != nil {
		return err
	}
	defer client.Release()
	format := wca.WAVEFORMATEX{WFormatTag: 1, NChannels: 2, NSamplesPerSec: 48000, NAvgBytesPerSec: 192000, NBlockAlign: 4, WBitsPerSample: 16}
	flags := uint32(wca.AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | wca.AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY)
	if err = client.Initialize(wca.AUDCLNT_SHAREMODE_SHARED, flags, 200000, 0, &format, nil); err != nil {
		return err
	}
	var render *wca.IAudioRenderClient
	if err = client.GetService(wca.IID_IAudioRenderClient, &render); err != nil {
		return err
	}
	defer render.Release()
	var capacity uint32
	if err = client.GetBufferSize(&capacity); err != nil {
		return err
	}
	ring := NewRing(19200)
	readDone := make(chan error, 1)
	go func() {
		var b [960]byte
		for {
			n, err := io.ReadFull(r, b[:])
			if n > 0 {
				ring.Write(b[:n])
			}
			if err != nil {
				readDone <- err
				return
			}
		}
	}()
	defer func() {
		if rc, ok := r.(io.Closer); ok {
			_ = rc.Close()
		}
	}()
	if err = client.Start(); err != nil {
		return err
	}
	defer client.Stop()
	clock := time.NewTicker(3 * time.Millisecond)
	defer clock.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err = <-readDone:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case <-clock.C:
			var padding uint32
			if err = client.GetCurrentPadding(&padding); err != nil {
				return err
			}
			if padding >= capacity {
				continue
			}
			frames := capacity - padding
			var data *byte
			if err = render.GetBuffer(frames, &data); err != nil {
				return err
			}
			out := unsafe.Slice(data, int(frames)*4)
			n := ring.Read(out)
			clear(out[n:])
			if err = render.ReleaseBuffer(frames, 0); err != nil {
				return err
			}
		}
	}
}
