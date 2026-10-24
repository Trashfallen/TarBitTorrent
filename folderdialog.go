package main

// Системный диалог выбора папки (IFileOpenDialog с FOS_PICKFOLDERS) - тот же,
// что в проводнике. Свой вызов COM: в готовой библиотеке методы получали
// неполные аргументы и программа падала при выборе начальной папки.

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modOle32                        = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx              = modOle32.NewProc("CoInitializeEx")
	procCoUninitialize              = modOle32.NewProc("CoUninitialize")
	procCoCreateInstance            = modOle32.NewProc("CoCreateInstance")
	procCoTaskMemFree               = modOle32.NewProc("CoTaskMemFree")
	procSHCreateItemFromParsingName = modShell32.NewProc("SHCreateItemFromParsingName")

	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidFileOpenDialog   = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidShellItem        = windows.GUID{Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE, Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

// Номера методов в таблице COM-интерфейсов.
const (
	vRelease        = 2
	vShow           = 3
	vSetOptions     = 9
	vGetOptions     = 10
	vSetFolder      = 12
	vSetTitle       = 17
	vGetResult      = 20
	vGetDisplayName = 5 // IShellItem

	fosPickFolders     = 0x20
	fosForceFileSystem = 0x40
	fosNoChangeDir     = 0x8
	sigdnFileSysPath   = 0x80058000
	hresultCancelled   = 0x800704C7
	clsctxInprocServer = 1
	coinitApartment    = 0x2
	coinitDisableOle1  = 0x4
)

// comCall вызывает метод n COM-объекта obj (первый аргумент - сам объект).
func comCall(obj unsafe.Pointer, n int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(n)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func hrErr(what string, hr uintptr) error {
	return errors.New(what + ": " + windows.Errno(hr&0xFFFF).Error())
}

// pickFolder: пустая строка без ошибки - пользователь нажал «Отмена».
func pickFolder(owner uintptr, initial string) (string, error) {
	type result struct {
		path string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		// диалог живет в одном потоке от создания до закрытия
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		p, err := pickFolderOnThread(owner, initial)
		ch <- result{p, err}
	}()
	r := <-ch
	return r.path, r.err
}

func pickFolderOnThread(owner uintptr, initial string) (string, error) {
	if hr, _, _ := procCoInitializeEx.Call(0, coinitApartment|coinitDisableOle1); int32(hr) >= 0 {
		defer procCoUninitialize.Call()
	}
	var dlg unsafe.Pointer
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidFileOpenDialog)), uintptr(unsafe.Pointer(&dlg)))
	if int32(hr) < 0 || dlg == nil {
		return "", hrErr("диалог выбора папки", hr)
	}
	defer comCall(dlg, vRelease)

	var opts uint32
	comCall(dlg, vGetOptions, uintptr(unsafe.Pointer(&opts)))
	comCall(dlg, vSetOptions, uintptr(opts|fosPickFolders|fosForceFileSystem|fosNoChangeDir))
	title, _ := windows.UTF16PtrFromString("Папка для загрузок")
	comCall(dlg, vSetTitle, uintptr(unsafe.Pointer(title)))
	if initial != "" && fileExists(initial) {
		p, _ := windows.UTF16PtrFromString(initial)
		var item unsafe.Pointer
		hr, _, _ := procSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(p)), 0,
			uintptr(unsafe.Pointer(&iidShellItem)), uintptr(unsafe.Pointer(&item)))
		if int32(hr) >= 0 && item != nil {
			comCall(dlg, vSetFolder, uintptr(item))
			comCall(item, vRelease)
		}
		runtime.KeepAlive(p)
	}
	runtime.KeepAlive(title)

	hr = comCall(dlg, vShow, owner)
	if uint32(hr) == hresultCancelled {
		return "", nil
	}
	if int32(hr) < 0 {
		return "", hrErr("диалог выбора папки", hr)
	}
	var item unsafe.Pointer
	if hr := comCall(dlg, vGetResult, uintptr(unsafe.Pointer(&item))); int32(hr) < 0 || item == nil {
		return "", hrErr("выбранная папка", hr)
	}
	defer comCall(item, vRelease)
	var name *uint16
	if hr := comCall(item, vGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))); int32(hr) < 0 || name == nil {
		return "", errors.New("выбрана не папка на диске")
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(name)))
	return windows.UTF16PtrToString(name), nil
}
