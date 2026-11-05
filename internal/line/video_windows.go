package line

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A video's preview is the frame Windows shows for it in File Explorer, from
// the Shell's thumbnail handlers (IShellItemImageFactory), so line-cli needs
// no video decoder of its own.

var (
	modOle32   = windows.NewLazySystemDLL("ole32.dll")
	modShell32 = windows.NewLazySystemDLL("shell32.dll")
	modGdi32   = windows.NewLazySystemDLL("gdi32.dll")
	modUser32  = windows.NewLazySystemDLL("user32.dll")

	procCoInitializeEx              = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize              = modOle32.NewProc("CoUninitialize")
	procSHCreateItemFromParsingName = modShell32.NewProc("SHCreateItemFromParsingName")
	procGetObjectW                  = modGdi32.NewProc("GetObjectW")
	procGetDIBits                   = modGdi32.NewProc("GetDIBits")
	procDeleteObject                = modGdi32.NewProc("DeleteObject")
	procGetDC                       = modUser32.NewProc("GetDC")
	procReleaseDC                   = modUser32.NewProc("ReleaseDC")
)

// IID_IShellItemImageFactory {bcc18b79-ba16-442f-80c4-8a59c30c463b}.
var iidShellItemImageFactory = windows.GUID{Data1: 0xbcc18b79, Data2: 0xba16, Data3: 0x442f, Data4: [8]byte{0x80, 0xc4, 0x8a, 0x59, 0xc3, 0x0c, 0x46, 0x3b}}

const (
	coinitApartmentThreaded = 0x2
	rpcEChangedMode         = 0x80010106
	siigbfBiggerSizeOK      = 0x1
	siigbfThumbnailOnly     = 0x8 // a real frame, never the file type's icon
)

type imageFactory struct{ vtbl *imageFactoryVtbl }

type imageFactoryVtbl struct {
	_, _, release, getImage uintptr // after QueryInterface and AddRef
}

type gdiBitmap struct {
	typ, width, height, widthBytes int32
	planes, bitsPixel              uint16
	bits                           uintptr
}

type bitmapInfo struct {
	size                   uint32
	width, height          int32
	planes, bitCount       uint16
	compression, sizeImage uint32
	xPelsPerM, yPelsPerM   int32
	clrUsed, clrImportant  uint32
	colors                 [4]byte
}

// videoFrame returns the preview frame Windows makes for a video, at least
// side pixels on its longer side when the video is that large.
func videoFrame(path string, side int, timeout time.Duration) (image.Image, error) {
	type result struct {
		img image.Image
		err error
	}
	ch := make(chan result, 1)
	go func() {
		img, err := shellThumbnail(path, side)
		ch <- result{img, err}
	}()
	select {
	case r := <-ch:
		return r.img, r.err
	case <-time.After(timeout):
		return nil, errors.New("Windows did not make a preview frame in time")
	}
}

func shellThumbnail(path string, side int) (image.Image, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded); uint32(hr) != rpcEChangedMode && int32(hr) >= 0 {
		defer procCoUninitialize.Call()
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var f *imageFactory
	hr, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&iidShellItemImageFactory)), uintptr(unsafe.Pointer(&f)))
	if int32(hr) < 0 || f == nil {
		return nil, fmt.Errorf("Windows cannot open the video (HRESULT 0x%08x)", uint32(hr))
	}
	defer syscall.SyscallN(f.vtbl.release, uintptr(unsafe.Pointer(f)))
	var hbmp uintptr
	size := uintptr(uint32(side)) | uintptr(uint32(side))<<32 // SIZE{cx, cy} by value
	hr, _, _ = syscall.SyscallN(f.vtbl.getImage, uintptr(unsafe.Pointer(f)), size, siigbfThumbnailOnly|siigbfBiggerSizeOK, uintptr(unsafe.Pointer(&hbmp)))
	if int32(hr) < 0 || hbmp == 0 {
		return nil, fmt.Errorf("Windows has no preview frame for this video (HRESULT 0x%08x); a codec may be missing", uint32(hr))
	}
	defer procDeleteObject.Call(hbmp)
	var bm gdiBitmap
	if r, _, _ := procGetObjectW.Call(hbmp, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); r == 0 || bm.width <= 0 || bm.height <= 0 {
		return nil, errors.New("Windows returned an unreadable preview frame")
	}
	w, h := int(bm.width), int(bm.height)
	if w*h > maxPhotoPixels {
		return nil, errors.New("Windows returned an oversized preview frame")
	}
	info := bitmapInfo{width: int32(w), height: -int32(h), planes: 1, bitCount: 32} // top-down BGRA
	info.size = uint32(unsafe.Offsetof(info.colors))
	buf := make([]byte, 4*w*h)
	hdc, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, hdc)
	if r, _, _ := procGetDIBits.Call(hdc, hbmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&info)), 0); int(r) != h {
		return nil, errors.New("Windows returned an unreadable preview frame")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[4*i], img.Pix[4*i+1], img.Pix[4*i+2], img.Pix[4*i+3] = buf[4*i+2], buf[4*i+1], buf[4*i], 255
	}
	return img, nil
}
