//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	appTitle = "网卡智能关机助手 V1.6"

	WS_OVERLAPPED  = 0x00000000
	WS_CAPTION     = 0x00C00000
	WS_SYSMENU     = 0x00080000
	WS_MINIMIZEBOX = 0x00020000
	WS_MAXIMIZEBOX = 0x00010000
	WS_THICKFRAME  = 0x00040000
	WS_VSCROLL     = 0x00200000
	WS_CHILD       = 0x40000000
	WS_VISIBLE     = 0x10000000
	WS_BORDER      = 0x00800000
	WS_TABSTOP     = 0x00010000

	BS_PUSHBUTTON    = 0x00000000
	BS_DEFPUSHBUTTON = 0x00000001
	BS_GROUPBOX      = 0x00000007
	BS_AUTOCHECKBOX  = 0x00000003

	ES_LEFT        = 0x0000
	ES_CENTER      = 0x0001
	ES_AUTOHSCROLL = 0x0080
	ES_READONLY    = 0x0800
	ES_MULTILINE   = 0x0004
	ES_AUTOVSCROLL = 0x0040

	CBS_DROPDOWNLIST = 0x0003

	WM_CREATE        = 0x0001
	WM_DESTROY       = 0x0002
	WM_SIZE          = 0x0005
	WM_GETMINMAXINFO = 0x0024
	WM_CLOSE         = 0x0010
	WM_COMMAND       = 0x0111
	WM_TIMER         = 0x0113
	WM_SETFONT       = 0x0030

	WM_APP_SAMPLE_READY = 0x8001

	BN_CLICKED    = 0
	CBN_SELCHANGE = 1

	SW_SHOW       = 5
	SW_SHOWNORMAL = 1

	SIZE_MINIMIZED = 1

	MB_OK              = 0x00000000
	MB_ICONINFORMATION = 0x00000040
	MB_ICONWARNING     = 0x00000030
	MB_ICONERROR       = 0x00000010
	MB_YESNO           = 0x00000004
	MB_ICONQUESTION    = 0x00000020
	IDYES              = 6

	CB_ADDSTRING = 0x0143
	CB_GETCURSEL = 0x0147
	CB_SETCURSEL = 0x014E

	BM_GETCHECK = 0x00F0
	BM_SETCHECK = 0x00F1
	BST_CHECKED = 1

	COLOR_WINDOW = 5

	FW_NORMAL   = 400
	FW_SEMIBOLD = 600
	FW_BOLD     = 700

	DEFAULT_CHARSET     = 1
	OUT_DEFAULT_PRECIS  = 0
	CLIP_DEFAULT_PRECIS = 0
	CLEARTYPE_QUALITY   = 5
	DEFAULT_PITCH       = 0

	IDC_ARROW = 32512

	ERROR_BUFFER_OVERFLOW     = 111
	ERROR_INSUFFICIENT_BUFFER = 122
	NO_ERROR                  = 0

	IF_TYPE_SOFTWARE_LOOPBACK      = 24
	MIB_IF_OPER_STATUS_OPERATIONAL = 5

	timerID = 1001

	idMode          = 1201
	idCountdown     = 1202
	idSchedule      = 1203
	idThreshold     = 1204
	idUnit          = 1205
	idStableSeconds = 1206
	idShutdownDelay = 1207
	idStart         = 1301
	idStop          = 1302
	idOpenLog       = 1303
	idAutoAbort     = 1501
	idGPUSelect     = 1601
	idGPUUtil       = 1602
	idGPUVRAM       = 1603
)

const (
	modeCountdown = iota
	modeSchedule
	modeAllBelow
	modeAllAbove
	modeGPUBelowEither
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type minmaxinfo struct {
	PtReserved     point
	PtMaxSize      point
	PtMaxPosition  point
	PtMinTrackSize point
	PtMaxTrackSize point
}
type msg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}
type wndclassex struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type mibIfRow struct {
	Name            [256]uint16
	Index           uint32
	Type            uint32
	Mtu             uint32
	Speed           uint32
	PhysAddrLen     uint32
	PhysAddr        [8]byte
	AdminStatus     uint32
	OperStatus      uint32
	LastChange      uint32
	InOctets        uint32
	InUcastPkts     uint32
	InNUcastPkts    uint32
	InDiscards      uint32
	InErrors        uint32
	InUnknownProtos uint32
	OutOctets       uint32
	OutUcastPkts    uint32
	OutNUcastPkts   uint32
	OutDiscards     uint32
	OutErrors       uint32
	OutQLen         uint32
	DescrLen        uint32
	Descr           [256]byte
}

type counterSnapshot struct {
	In  uint32
	Out uint32
	At  time.Time
}

type ratePair struct {
	In    float64
	Out   float64
	Total float64
}

// gpuAdapter 来自 DXGI，LUID 用于和 Windows GPU Performance Counter 对应。
type gpuAdapter struct {
	Index          int
	Name           string
	LuidLow        uint32
	LuidHigh       uint32
	DedicatedBytes uint64
}

type gpuSample struct {
	Index         int
	Name          string
	UtilPercent   float64
	MemoryUsed    uint64
	MemoryTotal   uint64
	MemoryPercent float64
	UtilValid     bool
	MemoryValid   bool
}

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type dxgiAdapterDesc1 struct {
	Description           [128]uint16
	VendorID              uint32
	DeviceID              uint32
	SubSysID              uint32
	Revision              uint32
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	AdapterLuidLow        uint32
	AdapterLuidHigh       int32
	Flags                 uint32
}

type pdhFmtCounterValue struct {
	CStatus     uint32
	_           uint32
	DoubleValue float64
}

type pdhFmtCounterValueItem struct {
	Name  *uint16
	Value pdhFmtCounterValue
}

type gpuCounterMonitor struct {
	query       uintptr
	utilCounter uintptr
	memCounter  uintptr
	primed      bool
}

type sampleResult struct {
	Download    float64
	Upload      float64
	Total       float64
	ActiveCount int
	Ready       bool
	Err         error
	SampledAt   time.Time
	GPUs        []gpuSample
	GPUErr      error
}

type monitorPlan struct {
	Active               bool
	Mode                 int
	ThresholdBytesPerSec float64
	StableDuration       time.Duration
	ShutdownDelaySec     int
	TargetTime           time.Time
	ConditionSince       time.Time
	Description          string
	GPUIndex             int
	GPUName              string
	GPUUtilThreshold     float64
	GPUVRAMThreshold     float64
}

type settings struct {
	Mode          int    `json:"mode"`
	Countdown     string `json:"countdown"`
	Schedule      string `json:"schedule"`
	Threshold     string `json:"threshold"`
	Unit          int    `json:"unit"`
	StableSeconds string `json:"stable_seconds"`
	ShutdownDelay string `json:"shutdown_delay"`
	AutoAbort     bool   `json:"auto_abort"`
	GPUIndex      int    `json:"gpu_index"`
	GPUUtil       string `json:"gpu_util_threshold"`
	GPUVRAM       string `json:"gpu_vram_threshold"`
}

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")
	modGdi32    = syscall.NewLazyDLL("gdi32.dll")
	modShell32  = syscall.NewLazyDLL("shell32.dll")
	modIphlpapi = syscall.NewLazyDLL("iphlpapi.dll")
	modDxgi     = syscall.NewLazyDLL("dxgi.dll")
	modPdh      = syscall.NewLazyDLL("pdh.dll")

	procRegisterClassExW     = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW      = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW       = modUser32.NewProc("DefWindowProcW")
	procShowWindow           = modUser32.NewProc("ShowWindow")
	procUpdateWindow         = modUser32.NewProc("UpdateWindow")
	procGetMessageW          = modUser32.NewProc("GetMessageW")
	procTranslateMessage     = modUser32.NewProc("TranslateMessage")
	procDispatchMessageW     = modUser32.NewProc("DispatchMessageW")
	procPostQuitMessage      = modUser32.NewProc("PostQuitMessage")
	procPostMessageW         = modUser32.NewProc("PostMessageW")
	procLoadCursorW          = modUser32.NewProc("LoadCursorW")
	procSetTimer             = modUser32.NewProc("SetTimer")
	procKillTimer            = modUser32.NewProc("KillTimer")
	procSendMessageW         = modUser32.NewProc("SendMessageW")
	procSetWindowTextW       = modUser32.NewProc("SetWindowTextW")
	procGetWindowTextLengthW = modUser32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW       = modUser32.NewProc("GetWindowTextW")
	procMessageBoxW          = modUser32.NewProc("MessageBoxW")
	procEnableWindow         = modUser32.NewProc("EnableWindow")
	procSetProcessDPIAware   = modUser32.NewProc("SetProcessDPIAware")
	procGetClientRect        = modUser32.NewProc("GetClientRect")
	procMoveWindow           = modUser32.NewProc("MoveWindow")

	procGetModuleHandleW             = modKernel32.NewProc("GetModuleHandleW")
	procCreateFontW                  = modGdi32.NewProc("CreateFontW")
	procDeleteObject                 = modGdi32.NewProc("DeleteObject")
	procShellExecuteW                = modShell32.NewProc("ShellExecuteW")
	procGetIfTable                   = modIphlpapi.NewProc("GetIfTable")
	procCreateDXGIFactory1           = modDxgi.NewProc("CreateDXGIFactory1")
	procPdhOpenQueryW                = modPdh.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCounterW        = modPdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData          = modPdh.NewProc("PdhCollectQueryData")
	procPdhGetFormattedCounterArrayW = modPdh.NewProc("PdhGetFormattedCounterArrayW")
	procPdhCloseQuery                = modPdh.NewProc("PdhCloseQuery")

	mainHwnd   uintptr
	fontNormal uintptr
	fontTitle  uintptr
	fontSpeed  uintptr
	fontSmall  uintptr

	hTitle      uintptr
	hSubtitle   uintptr
	hSpeedGroup uintptr
	hGPUGroup   uintptr
	hRuleGroup  uintptr
	hOpenLog    uintptr

	hDownload      uintptr
	hUpload        uintptr
	hTotal         uintptr
	hSampleInfo    uintptr
	hGPUCount      uintptr
	hGPUInfo       uintptr
	hMode          uintptr
	hCountdown     uintptr
	hSchedule      uintptr
	hThreshold     uintptr
	hUnit          uintptr
	hStableSeconds uintptr
	hShutdownDelay uintptr
	hStart         uintptr
	hStop          uintptr
	hStatus        uintptr
	hDetail        uintptr
	hAutoAbort     uintptr
	hGPUSelect     uintptr
	hGPUUtil       uintptr
	hGPUVRAM       uintptr
	hDelayHint     uintptr

	latestSample  sampleResult
	gpuAdapters   []gpuAdapter
	sampleResults chan sampleResult
	stopWorker    chan struct{}
	workerOnce    sync.Once
	notifyPending int32

	plan    monitorPlan
	stateMu sync.Mutex

	logFilePath    string
	configFilePath string

	initializationStarted     time.Time
	initializationSampleCount int
	initializationComplete    bool
	initializationTimedOut    bool
	lastInitializationError   string
)

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}
func loWord(v uintptr) uint16 { return uint16(v & 0xFFFF) }
func hiWord(v uintptr) uint16 { return uint16((v >> 16) & 0xFFFF) }

func createWindow(exStyle uint32, class, text string, style uint32, x, y, w, h int32, parent uintptr, id int32) uintptr {
	hwnd, _, _ := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(utf16Ptr(class))),
		uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, uintptr(id), 0, 0,
	)
	return hwnd
}

func setFont(hwnd, font uintptr) { procSendMessageW.Call(hwnd, WM_SETFONT, font, 1) }
func setText(hwnd uintptr, text string) {
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))))
}
func getText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}
func messageBox(hwnd uintptr, text, title string, flags uint32) int {
	r, _, _ := procMessageBoxW.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), uintptr(flags))
	return int(r)
}
func addComboItem(hwnd uintptr, text string) {
	procSendMessageW.Call(hwnd, CB_ADDSTRING, 0, uintptr(unsafe.Pointer(utf16Ptr(text))))
}
func comboSelection(hwnd uintptr) int {
	r, _, _ := procSendMessageW.Call(hwnd, CB_GETCURSEL, 0, 0)
	return int(r)
}
func setComboSelection(hwnd uintptr, idx int) {
	procSendMessageW.Call(hwnd, CB_SETCURSEL, uintptr(idx), 0)
}
func checkState(hwnd uintptr) bool {
	r, _, _ := procSendMessageW.Call(hwnd, BM_GETCHECK, 0, 0)
	return r == BST_CHECKED
}
func setCheck(hwnd uintptr, checked bool) {
	v := uintptr(0)
	if checked {
		v = BST_CHECKED
	}
	procSendMessageW.Call(hwnd, BM_SETCHECK, v, 0)
}
func enableWindow(hwnd uintptr, enabled bool) {
	v := uintptr(0)
	if enabled {
		v = 1
	}
	procEnableWindow.Call(hwnd, v)
}

var (
	gpuInstanceRe = regexp.MustCompile(`(?i)luid_0x([0-9a-f]+)_0x([0-9a-f]+)_phys_([0-9]+)`)
	gpuEngineRe   = regexp.MustCompile(`(?i)_eng_([^_]+)_engtype_`)
)

const (
	dxgiErrorNotFound       = 0x887A0002
	dxgiAdapterFlagSoftware = 0x2
	pdhFmtDouble            = 0x00000200
	pdhMoreData             = 0x800007D2
)

func hresultFailed(v uintptr) bool { return int32(uint32(v)) < 0 }

func comMethod(obj uintptr, index uintptr, args ...uintptr) uintptr {
	if obj == 0 {
		return ^uintptr(0)
	}
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + index*unsafe.Sizeof(uintptr(0))))
	all := make([]uintptr, 0, len(args)+1)
	all = append(all, obj)
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return r
}

func comRelease(obj uintptr) {
	if obj != 0 {
		_ = comMethod(obj, 2)
	}
}

func enumerateGPUAdapters() ([]gpuAdapter, error) {
	iid := guid{
		Data1: 0x770aae78,
		Data2: 0xf26f,
		Data3: 0x4dba,
		Data4: [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87},
	}
	var factory uintptr
	hr, _, err := procCreateDXGIFactory1.Call(uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&factory)))
	if hresultFailed(hr) || factory == 0 {
		return nil, fmt.Errorf("CreateDXGIFactory1 失败: 0x%08X (%v)", uint32(hr), err)
	}
	defer comRelease(factory)

	adapters := make([]gpuAdapter, 0, 4)
	for i := 0; i < 32; i++ {
		var adapter uintptr
		hr = comMethod(factory, 12, uintptr(i), uintptr(unsafe.Pointer(&adapter))) // IDXGIFactory1::EnumAdapters1
		if uint32(hr) == dxgiErrorNotFound {
			break
		}
		if hresultFailed(hr) || adapter == 0 {
			continue
		}
		var desc dxgiAdapterDesc1
		descHR := comMethod(adapter, 10, uintptr(unsafe.Pointer(&desc))) // IDXGIAdapter1::GetDesc1
		comRelease(adapter)
		if hresultFailed(descHR) || (desc.Flags&dxgiAdapterFlagSoftware) != 0 {
			continue
		}
		name := strings.TrimSpace(syscall.UTF16ToString(desc.Description[:]))
		if name == "" {
			name = fmt.Sprintf("GPU %d", len(adapters))
		}
		adapters = append(adapters, gpuAdapter{
			Index:          len(adapters),
			Name:           name,
			LuidLow:        desc.AdapterLuidLow,
			LuidHigh:       uint32(desc.AdapterLuidHigh),
			DedicatedBytes: uint64(desc.DedicatedVideoMemory),
		})
	}
	if len(adapters) == 0 {
		return nil, fmt.Errorf("未检测到可用的 DXGI 显卡适配器")
	}
	return adapters, nil
}

func luidKey(high, low uint32) string {
	return fmt.Sprintf("%08x:%08x", high, low)
}

func parseCounterLUID(instance string, known map[string]struct{}) (string, string, bool) {
	m := gpuInstanceRe.FindStringSubmatch(instance)
	if len(m) != 4 {
		return "", "", false
	}
	a64, err1 := strconv.ParseUint(m[1], 16, 32)
	b64, err2 := strconv.ParseUint(m[2], 16, 32)
	if err1 != nil || err2 != nil {
		return "", "", false
	}
	a, b := uint32(a64), uint32(b64)
	k1 := luidKey(a, b)
	k2 := luidKey(b, a)
	key := k1
	if _, ok := known[k1]; !ok {
		if _, ok2 := known[k2]; ok2 {
			key = k2
		}
	}
	return key, m[3], true
}

func newGPUCounterMonitor() (*gpuCounterMonitor, error) {
	m := &gpuCounterMonitor{}
	status, _, _ := procPdhOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&m.query)))
	if uint32(status) != 0 || m.query == 0 {
		return nil, fmt.Errorf("PdhOpenQueryW 错误码 0x%08X", uint32(status))
	}
	cleanup := func() {
		if m.query != 0 {
			procPdhCloseQuery.Call(m.query)
			m.query = 0
		}
	}
	status, _, _ = procPdhAddEnglishCounterW.Call(m.query, uintptr(unsafe.Pointer(utf16Ptr(`\GPU Engine(*)\Utilization Percentage`))), 0, uintptr(unsafe.Pointer(&m.utilCounter)))
	if uint32(status) != 0 {
		cleanup()
		return nil, fmt.Errorf("无法添加 GPU 使用率计数器: 0x%08X", uint32(status))
	}
	status, _, _ = procPdhAddEnglishCounterW.Call(m.query, uintptr(unsafe.Pointer(utf16Ptr(`\GPU Adapter Memory(*)\Dedicated Usage`))), 0, uintptr(unsafe.Pointer(&m.memCounter)))
	if uint32(status) != 0 {
		cleanup()
		return nil, fmt.Errorf("无法添加 GPU 显存计数器: 0x%08X", uint32(status))
	}
	status, _, _ = procPdhCollectQueryData.Call(m.query)
	if uint32(status) != 0 {
		cleanup()
		return nil, fmt.Errorf("GPU 性能计数器初始化失败: 0x%08X", uint32(status))
	}
	m.primed = true
	return m, nil
}

func (m *gpuCounterMonitor) close() {
	if m != nil && m.query != 0 {
		procPdhCloseQuery.Call(m.query)
		m.query = 0
	}
}

type namedCounterValue struct {
	Name  string
	Value float64
	Valid bool
}

func utf16PtrString(p *uint16) string {
	if p == nil {
		return ""
	}
	buf := (*[4096]uint16)(unsafe.Pointer(p))
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(buf[:n])
}

func readPDHCounterArray(counter uintptr) ([]namedCounterValue, error) {
	var bufSize uint32
	var count uint32
	status, _, _ := procPdhGetFormattedCounterArrayW.Call(counter, pdhFmtDouble, uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&count)), 0)
	if uint32(status) != pdhMoreData && uint32(status) != 0 {
		return nil, fmt.Errorf("PdhGetFormattedCounterArrayW 错误码 0x%08X", uint32(status))
	}
	if bufSize == 0 || count == 0 {
		return nil, nil
	}
	buf := make([]byte, bufSize)
	status, _, _ = procPdhGetFormattedCounterArrayW.Call(counter, pdhFmtDouble, uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if uint32(status) != 0 {
		return nil, fmt.Errorf("读取 GPU 性能数据失败: 0x%08X", uint32(status))
	}
	itemSize := unsafe.Sizeof(pdhFmtCounterValueItem{})
	values := make([]namedCounterValue, 0, count)
	for i := uint32(0); i < count; i++ {
		item := (*pdhFmtCounterValueItem)(unsafe.Pointer(uintptr(unsafe.Pointer(&buf[0])) + uintptr(i)*itemSize))
		valid := item.Value.CStatus == 0 || item.Value.CStatus == 1
		values = append(values, namedCounterValue{Name: utf16PtrString(item.Name), Value: item.Value.DoubleValue, Valid: valid})
	}
	return values, nil
}

func (m *gpuCounterMonitor) collect(adapters []gpuAdapter) ([]gpuSample, error) {
	if m == nil || m.query == 0 {
		return nil, fmt.Errorf("GPU 性能计数器未初始化")
	}
	status, _, _ := procPdhCollectQueryData.Call(m.query)
	if uint32(status) != 0 {
		return nil, fmt.Errorf("PdhCollectQueryData 错误码 0x%08X", uint32(status))
	}
	utilValues, err := readPDHCounterArray(m.utilCounter)
	if err != nil {
		return nil, err
	}
	memValues, err := readPDHCounterArray(m.memCounter)
	if err != nil {
		return nil, err
	}

	known := make(map[string]struct{}, len(adapters))
	for _, a := range adapters {
		known[luidKey(a.LuidHigh, a.LuidLow)] = struct{}{}
	}
	engineSums := make(map[string]map[string]float64)
	for _, v := range utilValues {
		if !v.Valid {
			continue
		}
		key, phys, ok := parseCounterLUID(v.Name, known)
		if !ok {
			continue
		}
		eng := "all"
		if em := gpuEngineRe.FindStringSubmatch(v.Name); len(em) == 2 {
			eng = em[1]
		}
		engineKey := phys + ":" + eng
		if engineSums[key] == nil {
			engineSums[key] = make(map[string]float64)
		}
		engineSums[key][engineKey] += v.Value
	}
	utilByGPU := make(map[string]float64)
	utilValid := make(map[string]bool)
	for key, engines := range engineSums {
		maxValue := 0.0
		for _, value := range engines {
			if value > maxValue {
				maxValue = value
			}
		}
		if maxValue > 100 {
			maxValue = 100
		}
		if maxValue < 0 {
			maxValue = 0
		}
		utilByGPU[key] = maxValue
		utilValid[key] = true
	}

	memByGPU := make(map[string]float64)
	memValid := make(map[string]bool)
	for _, v := range memValues {
		if !v.Valid || v.Value < 0 {
			continue
		}
		key, _, ok := parseCounterLUID(v.Name, known)
		if !ok {
			continue
		}
		memByGPU[key] += v.Value
		memValid[key] = true
	}

	samples := make([]gpuSample, 0, len(adapters))
	for _, a := range adapters {
		key := luidKey(a.LuidHigh, a.LuidLow)
		s := gpuSample{Index: a.Index, Name: a.Name, MemoryTotal: a.DedicatedBytes}
		if utilValid[key] {
			s.UtilPercent = utilByGPU[key]
			s.UtilValid = true
		}
		if memValid[key] {
			used := memByGPU[key]
			if used < 0 {
				used = 0
			}
			s.MemoryUsed = uint64(used)
			if s.MemoryTotal > 0 {
				s.MemoryPercent = float64(s.MemoryUsed) * 100 / float64(s.MemoryTotal)
				if s.MemoryPercent > 100 {
					s.MemoryPercent = 100
				}
				s.MemoryValid = true
			}
		}
		samples = append(samples, s)
	}
	return samples, nil
}

func formatBytes(v uint64) string {
	value := float64(v)
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for value >= 1024 && i < len(units)-1 {
		value /= 1024
		i++
	}
	if i <= 1 {
		return fmt.Sprintf("%.0f %s", value, units[i])
	}
	return fmt.Sprintf("%.2f %s", value, units[i])
}

func main() {
	// Win32 窗口必须由固定的 OS 线程创建并持续处理消息。
	// V1.2 未锁定线程，在部分 Win11 环境中可能数秒后出现“未响应”。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	exe, _ := os.Executable()
	base := filepath.Dir(exe)
	logFilePath = filepath.Join(base, "网卡智能关机助手.log")
	configFilePath = filepath.Join(base, "settings.json")

	procSetProcessDPIAware.Call()
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
	className := utf16Ptr("NetShutdownAssistantV16Class")
	wndProcCallback := syscall.NewCallback(wndProc)

	wc := wndclassex{
		CbSize:        uint32(unsafe.Sizeof(wndclassex{})),
		LpfnWndProc:   wndProcCallback,
		HInstance:     hInstance,
		HCursor:       cursor,
		HbrBackground: COLOR_WINDOW + 1,
		LpszClassName: className,
	}
	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		fmt.Fprintf(os.Stderr, "RegisterClassExW failed: %v\n", err)
		return
	}

	mainHwnd = createWindow(0, "NetShutdownAssistantV16Class", appTitle+" · Windows 11", WS_OVERLAPPED|WS_CAPTION|WS_SYSMENU|WS_MINIMIZEBOX|WS_MAXIMIZEBOX|WS_THICKFRAME, 180, 80, 720, 900, 0, 0)
	if mainHwnd == 0 {
		return
	}

	procShowWindow.Call(mainHwnd, SW_SHOW)
	procUpdateWindow.Call(mainHwnd)

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_CREATE:
		onCreate(hwnd)
		return 0
	case WM_COMMAND:
		onCommand(int(loWord(wParam)), int(hiWord(wParam)))
		return 0
	case WM_SIZE:
		if wParam != SIZE_MINIMIZED {
			layoutControls(hwnd)
		}
		return 0
	case WM_GETMINMAXINFO:
		if lParam != 0 {
			mmi := (*minmaxinfo)(unsafe.Pointer(lParam))
			mmi.PtMinTrackSize.X = 690
			mmi.PtMinTrackSize.Y = 820
		}
		return 0
	case WM_TIMER:
		if wParam == timerID {
			onTimer()
		}
		return 0
	case WM_APP_SAMPLE_READY:
		atomic.StoreInt32(&notifyPending, 0)
		applyLatestSample()
		return 0
	case WM_CLOSE:
		stateMu.Lock()
		active := plan.Active
		stateMu.Unlock()
		if active && messageBox(hwnd, "监控任务仍在运行。确定要退出吗？\n\n退出后将不再继续判断关机条件。", "确认退出", MB_YESNO|MB_ICONQUESTION) != IDYES {
			return 0
		}
		procKillTimer.Call(hwnd, timerID)
		stopSamplerWorker()
		procPostQuitMessage.Call(0)
		return 0
	case WM_DESTROY:
		stopSamplerWorker()
		for _, f := range []uintptr{fontNormal, fontTitle, fontSpeed, fontSmall} {
			if f != 0 {
				procDeleteObject.Call(f)
			}
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func createFont(height int32, weight int32, face string) uintptr {
	r, _, _ := procCreateFontW.Call(uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0, DEFAULT_CHARSET, OUT_DEFAULT_PRECIS, CLIP_DEFAULT_PRECIS, CLEARTYPE_QUALITY, DEFAULT_PITCH, uintptr(unsafe.Pointer(utf16Ptr(face))))
	return r
}

func onCreate(hwnd uintptr) {
	fontNormal = createFont(-16, FW_NORMAL, "Microsoft YaHei UI")
	fontSmall = createFont(-14, FW_NORMAL, "Microsoft YaHei UI")
	fontTitle = createFont(-25, FW_SEMIBOLD, "Microsoft YaHei UI")
	fontSpeed = createFont(-24, FW_BOLD, "Microsoft YaHei UI")

	hTitle = createWindow(0, "STATIC", "网卡智能关机助手", WS_CHILD|WS_VISIBLE, 24, 16, 420, 34, hwnd, 0)
	setFont(hTitle, fontTitle)
	hSubtitle = createWindow(0, "STATIC", "V1.6：网速 + 多显卡 GPU/显存低负载自动关机", WS_CHILD|WS_VISIBLE, 25, 50, 640, 24, hwnd, 0)
	setFont(hSubtitle, fontSmall)

	hSpeedGroup = createWindow(0, "BUTTON", " 当前网速 ", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 20, 82, 630, 126, hwnd, 0)
	setFont(hSpeedGroup, fontNormal)
	makeLabel(hwnd, "下载", 42, 112, 54, 24, fontSmall)
	hDownload = createWindow(0, "STATIC", "0 B/s", WS_CHILD|WS_VISIBLE|ES_CENTER, 96, 104, 130, 38, hwnd, 0)
	setFont(hDownload, fontSpeed)
	makeLabel(hwnd, "上传", 248, 112, 54, 24, fontSmall)
	hUpload = createWindow(0, "STATIC", "0 B/s", WS_CHILD|WS_VISIBLE|ES_CENTER, 302, 104, 130, 38, hwnd, 0)
	setFont(hUpload, fontSpeed)
	makeLabel(hwnd, "合计", 454, 112, 54, 24, fontSmall)
	hTotal = createWindow(0, "STATIC", "0 B/s", WS_CHILD|WS_VISIBLE|ES_CENTER, 508, 104, 120, 38, hwnd, 0)
	setFont(hTotal, fontSpeed)
	hSampleInfo = createWindow(0, "STATIC", "正在建立网速基线…", WS_CHILD|WS_VISIBLE, 42, 157, 586, 30, hwnd, 0)
	setFont(hSampleInfo, fontSmall)

	hGPUGroup = createWindow(0, "BUTTON", " GPU 监控 ", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 20, 220, 630, 128, hwnd, 0)
	setFont(hGPUGroup, fontNormal)
	hGPUCount = createWindow(0, "STATIC", "正在检测显卡…", WS_CHILD|WS_VISIBLE, 42, 244, 586, 22, hwnd, 0)
	setFont(hGPUCount, fontSmall)
	hGPUInfo = createWindow(0, "EDIT", "等待 GPU 性能采样…", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|ES_LEFT|ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY, 42, 270, 586, 64, hwnd, 0)
	setFont(hGPUInfo, fontSmall)

	hRuleGroup = createWindow(0, "BUTTON", " 关机规则 ", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 20, 360, 630, 300, hwnd, 0)
	setFont(hRuleGroup, fontNormal)
	makeLabel(hwnd, "执行模式", 42, 388, 86, 28, fontNormal)
	hMode = createWindow(0, "COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 132, 384, 390, 180, hwnd, idMode)
	setFont(hMode, fontNormal)
	addComboItem(hMode, "倒计时关机")
	addComboItem(hMode, "指定日期时间关机")
	addComboItem(hMode, "全部网卡合计低于阈值")
	addComboItem(hMode, "全部网卡合计高于阈值")
	addComboItem(hMode, "指定显卡：GPU使用率 或 显存使用率低于阈值")
	setComboSelection(hMode, 0)

	makeLabel(hwnd, "倒计时", 42, 426, 86, 28, fontNormal)
	hCountdown = makeEdit(hwnd, "01:00:00", 132, 422, 174, 30, idCountdown)
	hint := createWindow(0, "STATIC", "HH:MM:SS / 90m / 2h", WS_CHILD|WS_VISIBLE, 316, 427, 210, 24, hwnd, 0)
	setFont(hint, fontSmall)

	makeLabel(hwnd, "定时时间", 42, 464, 86, 28, fontNormal)
	hSchedule = makeEdit(hwnd, time.Now().Add(2*time.Hour).Format("2006-01-02 15:04:05"), 132, 460, 286, 30, idSchedule)

	makeLabel(hwnd, "网速阈值", 42, 502, 86, 28, fontNormal)
	hThreshold = makeEdit(hwnd, "100", 132, 498, 92, 30, idThreshold)
	hUnit = createWindow(0, "COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 232, 498, 92, 100, hwnd, idUnit)
	setFont(hUnit, fontNormal)
	addComboItem(hUnit, "KB/s")
	addComboItem(hUnit, "MB/s")
	setComboSelection(hUnit, 0)
	makeLabel(hwnd, "持续", 342, 502, 48, 28, fontNormal)
	hStableSeconds = makeEdit(hwnd, "60", 390, 498, 72, 30, idStableSeconds)
	makeLabel(hwnd, "秒", 470, 502, 32, 28, fontNormal)

	makeLabel(hwnd, "指定显卡", 42, 540, 86, 28, fontNormal)
	hGPUSelect = createWindow(0, "COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 132, 536, 390, 160, hwnd, idGPUSelect)
	setFont(hGPUSelect, fontNormal)

	makeLabel(hwnd, "GPU阈值", 42, 578, 86, 28, fontNormal)
	hGPUUtil = makeEdit(hwnd, "10", 132, 574, 62, 30, idGPUUtil)
	makeLabel(hwnd, "%  或  显存", 202, 578, 94, 28, fontSmall)
	hGPUVRAM = makeEdit(hwnd, "10", 296, 574, 62, 30, idGPUVRAM)
	makeLabel(hwnd, "%", 366, 578, 28, 28, fontSmall)
	gpuRuleHint := createWindow(0, "STATIC", "任一项低于阈值即可开始延迟计时", WS_CHILD|WS_VISIBLE, 408, 578, 214, 24, hwnd, 0)
	setFont(gpuRuleHint, fontSmall)

	makeLabel(hwnd, "关机延迟", 42, 616, 86, 28, fontNormal)
	hShutdownDelay = makeEdit(hwnd, "10", 132, 612, 72, 30, idShutdownDelay)
	hDelayHint = createWindow(0, "STATIC", "秒（触发后 Windows 倒计时）", WS_CHILD|WS_VISIBLE, 214, 616, 210, 24, hwnd, 0)
	setFont(hDelayHint, fontSmall)
	hAutoAbort = createWindow(0, "BUTTON", "停止任务时取消已排定关机", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 430, 612, 192, 30, hwnd, idAutoAbort)
	setFont(hAutoAbort, fontSmall)
	setCheck(hAutoAbort, true)

	hStart = createWindow(0, "BUTTON", "启动任务", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_DEFPUSHBUTTON, 20, 674, 170, 40, hwnd, idStart)
	setFont(hStart, fontNormal)
	hStop = createWindow(0, "BUTTON", "停止并取消", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 205, 674, 170, 40, hwnd, idStop)
	setFont(hStop, fontNormal)
	hOpenLog = createWindow(0, "BUTTON", "打开日志", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 390, 674, 130, 40, hwnd, idOpenLog)
	setFont(hOpenLog, fontNormal)

	hStatus = createWindow(0, "STATIC", "  状态：初始化中", WS_CHILD|WS_VISIBLE|WS_BORDER, 20, 728, 630, 31, hwnd, 0)
	setFont(hStatus, fontNormal)
	hDetail = createWindow(0, "EDIT", "正在建立网速和 GPU 性能基线。", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|ES_LEFT|ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY, 20, 768, 630, 90, hwnd, 0)
	setFont(hDetail, fontSmall)

	var gpuErr error
	gpuAdapters, gpuErr = enumerateGPUAdapters()
	if gpuErr != nil {
		setText(hGPUCount, "显卡检测失败："+gpuErr.Error())
		setText(hGPUInfo, "GPU 条件关机暂不可用；其他关机模式不受影响。")
	} else {
		setText(hGPUCount, fmt.Sprintf("检测到 %d 张显卡", len(gpuAdapters)))
		for _, gpu := range gpuAdapters {
			addComboItem(hGPUSelect, fmt.Sprintf("GPU %d · %s", gpu.Index, gpu.Name))
		}
		if len(gpuAdapters) > 0 {
			setComboSelection(hGPUSelect, 0)
		}
	}

	sampleResults = make(chan sampleResult, 1)
	stopWorker = make(chan struct{})
	loadSettings()
	updateModeControls()
	enableWindow(hStart, true)
	enableWindow(hStop, false)
	initializationStarted = time.Now()
	initializationSampleCount = 0
	initializationComplete = false
	initializationTimedOut = false
	lastInitializationError = ""
	setStatus("状态：初始化中 · 可先使用时间/GPU模式")
	setDetail("正在读取网卡计数器和 GPU 性能计数器。网速条件需等待网速初始化完成；GPU 模式需等待显卡实时数据出现。")
	startSamplerWorker()
	procSetTimer.Call(hwnd, timerID, 500, 0)
	layoutControls(hwnd)
	appendLog(fmt.Sprintf("程序启动：V1.6 GPU监控版，检测到%d张显卡", len(gpuAdapters)))
}

func moveWindow(hwnd uintptr, x, y, w, h int32) {
	if hwnd == 0 || w <= 0 || h <= 0 {
		return
	}
	procMoveWindow.Call(hwnd, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 1)
}

func layoutControls(hwnd uintptr) {
	if hwnd == 0 || hDetail == 0 {
		return
	}
	var rc rect
	if r, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); r == 0 {
		return
	}
	clientW := rc.Right - rc.Left
	clientH := rc.Bottom - rc.Top
	contentW := clientW - 40
	if contentW < 630 {
		contentW = 630
	}

	moveWindow(hTitle, 24, 16, clientW-48, 34)
	moveWindow(hSubtitle, 25, 50, clientW-50, 24)
	moveWindow(hSpeedGroup, 20, 82, contentW, 126)
	moveWindow(hSampleInfo, 42, 157, contentW-44, 30)
	moveWindow(hGPUGroup, 20, 220, contentW, 128)
	moveWindow(hGPUCount, 42, 244, contentW-44, 22)
	moveWindow(hGPUInfo, 42, 270, contentW-44, 64)
	moveWindow(hRuleGroup, 20, 360, contentW, 300)

	statusTop := int32(728)
	detailTop := int32(768)
	detailH := clientH - detailTop - 16
	if detailH < 70 {
		detailH = 70
	}
	moveWindow(hStatus, 20, statusTop, contentW, 31)
	moveWindow(hDetail, 20, detailTop, contentW, detailH)
}

func makeLabel(parent uintptr, text string, x, y, w, h int32, font uintptr) uintptr {
	c := createWindow(0, "STATIC", text, WS_CHILD|WS_VISIBLE, x, y, w, h, parent, 0)
	setFont(c, font)
	return c
}
func makeEdit(parent uintptr, text string, x, y, w, h int32, id int32) uintptr {
	c := createWindow(0, "EDIT", text, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP|ES_LEFT|ES_AUTOHSCROLL, x, y, w, h, parent, id)
	setFont(c, fontNormal)
	return c
}

func onCommand(id, code int) {
	switch id {
	case idMode:
		if code == CBN_SELCHANGE {
			updateModeControls()
		}
	case idStart:
		if code == BN_CLICKED {
			startMonitoring()
		}
	case idStop:
		if code == BN_CLICKED {
			stopMonitoring(true)
		}
	case idOpenLog:
		if code == BN_CLICKED {
			openPath(logFilePath)
		}
	}
}

func updateModeControls() {
	mode := comboSelection(hMode)
	enableWindow(hCountdown, mode == modeCountdown)
	enableWindow(hSchedule, mode == modeSchedule)
	speedMode := mode == modeAllBelow || mode == modeAllAbove
	enableWindow(hThreshold, speedMode)
	enableWindow(hUnit, speedMode)
	enableWindow(hStableSeconds, speedMode)
	gpuMode := mode == modeGPUBelowEither
	enableWindow(hGPUSelect, gpuMode && len(gpuAdapters) > 0)
	enableWindow(hGPUUtil, gpuMode)
	enableWindow(hGPUVRAM, gpuMode)
	if gpuMode {
		setText(hDelayHint, "秒（GPU条件连续满足后立即关机）")
	} else {
		setText(hDelayHint, "秒（触发后 Windows 倒计时）")
	}
}

func onTimer() {
	stateMu.Lock()
	activeNow := plan.Active
	stateMu.Unlock()
	if !initializationComplete {
		if !initializationStarted.IsZero() && time.Since(initializationStarted) >= 10*time.Second && !initializationTimedOut {
			initializationTimedOut = true
			if !activeNow {
				setStatus("状态：网速初始化超时 · 正在后台重试")
				if lastInitializationError != "" {
					setDetail("暂时无法建立网速基线：" + lastInitializationError + "。程序仍在后台每2秒重试；时间/GPU模式仍可使用。")
				} else {
					setDetail("已等待超过10秒，但尚未获得可用于计算网速的连续采样。程序仍在后台重试；时间/GPU模式仍可使用。")
				}
			}
			setText(hSampleInfo, "网速初始化超时，后台继续重试…")
			appendLog("网速初始化超过10秒，进入后台重试状态")
		}
	} else if !latestSample.SampledAt.IsZero() && time.Since(latestSample.SampledAt) > 8*time.Second {
		stateMu.Lock()
		if plan.Active && (plan.Mode == modeAllBelow || plan.Mode == modeAllAbove) {
			plan.ConditionSince = time.Time{}
		}
		stateMu.Unlock()
		setText(hSampleInfo, "网速采样暂时无响应；网速条件计时已暂停")
	}
	evaluatePlan()
}

func startSamplerWorker() { go samplerWorker() }
func stopSamplerWorker() {
	workerOnce.Do(func() {
		if stopWorker != nil {
			close(stopWorker)
		}
	})
}

func publishSample(result sampleResult) {
	select {
	case sampleResults <- result:
	default:
		select {
		case <-sampleResults:
		default:
		}
		select {
		case sampleResults <- result:
		default:
		}
	}
	if mainHwnd != 0 && atomic.CompareAndSwapInt32(&notifyPending, 0, 1) {
		procPostMessageW.Call(mainHwnd, WM_APP_SAMPLE_READY, 0, 0)
	}
}

func samplerWorker() {
	previous := make(map[uint32]counterSnapshot)
	gpuMon, gpuMonErr := newGPUCounterMonitor()
	if gpuMon != nil {
		defer gpuMon.close()
	}

	sample := func() {
		rows, netErr := getIfRows()
		now := time.Now()
		result := sampleResult{SampledAt: now}

		if netErr != nil {
			result.Err = netErr
		} else {
			grouped := make(map[string]ratePair)
			ready := false
			currentIDs := make(map[uint32]struct{}, len(rows))

			for _, row := range rows {
				currentIDs[row.Index] = struct{}{}
				if row.Type == IF_TYPE_SOFTWARE_LOOPBACK || row.OperStatus != MIB_IF_OPER_STATUS_OPERATIONAL {
					continue
				}
				desc := strings.TrimSpace(strings.ToLower(mibDescription(row)))
				if desc == "" {
					desc = fmt.Sprintf("if-%d", row.Index)
				}
				if isFilterOnlyAdapter(desc) {
					continue
				}
				prev, exists := previous[row.Index]
				previous[row.Index] = counterSnapshot{In: row.InOctets, Out: row.OutOctets, At: now}
				if !exists {
					continue
				}
				elapsed := now.Sub(prev.At).Seconds()
				if elapsed < 0.5 || elapsed > 10 {
					continue
				}
				inRate := float64(counterDelta(prev.In, row.InOctets)) / elapsed
				outRate := float64(counterDelta(prev.Out, row.OutOctets)) / elapsed
				pair := ratePair{In: inRate, Out: outRate, Total: inRate + outRate}
				old, ok := grouped[desc]
				if !ok || pair.Total > old.Total {
					grouped[desc] = pair
				}
				ready = true
			}
			for idx := range previous {
				if _, ok := currentIDs[idx]; !ok {
					delete(previous, idx)
				}
			}
			result.Ready = ready
			result.ActiveCount = len(grouped)
			for _, pair := range grouped {
				result.Download += pair.In
				result.Upload += pair.Out
			}
			result.Total = result.Download + result.Upload
		}

		if gpuMonErr != nil {
			result.GPUErr = gpuMonErr
		} else if len(gpuAdapters) > 0 {
			result.GPUs, result.GPUErr = gpuMon.collect(gpuAdapters)
		}
		publishSample(result)
	}

	sample()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stopWorker:
			return
		case <-ticker.C:
			sample()
		}
	}
}

func isFilterOnlyAdapter(desc string) bool {
	markers := []string{
		"lightweight filter", "wfp 802.3", "wfp native", "virtual switch extension adapter",
		"qos packet scheduler", "network monitor", "ndis capture", "loopback pseudo-interface",
	}
	for _, m := range markers {
		if strings.Contains(desc, m) {
			return true
		}
	}
	return false
}

func applyLatestSample() {
	var latest sampleResult
	got := false
	for {
		select {
		case latest = <-sampleResults:
			got = true
		default:
			if !got {
				return
			}
			goto APPLY
		}
	}
APPLY:
	latestSample = latest
	applyGPUDisplay(latest)

	if latest.Err != nil {
		errText := latest.Err.Error()
		setText(hSampleInfo, "网速读取失败："+errText)
		if !initializationComplete {
			stateMu.Lock()
			active := plan.Active
			stateMu.Unlock()
			if !active {
				setStatus("状态：网速初始化失败 · 时间/GPU模式仍可使用")
				setDetail("读取网卡计数器失败：" + errText + "。程序会每2秒自动重试；GPU模式不依赖网速。")
			}
			if errText != lastInitializationError {
				appendLog("初始化采样失败：" + errText)
				lastInitializationError = errText
			}
		} else {
			appendLog("网速采样失败：" + errText)
		}
		evaluatePlan()
		return
	}

	initializationSampleCount++
	lastInitializationError = ""
	if !latest.Ready {
		if initializationSampleCount <= 1 {
			setText(hSampleInfo, "第1次采样完成，正在等待第2次采样计算网速…")
		} else {
			setText(hSampleInfo, fmt.Sprintf("正在建立网速基线（已采样%d次），继续重试…", initializationSampleCount))
		}
		evaluatePlan()
		return
	}

	setText(hDownload, formatRate(latest.Download))
	setText(hUpload, formatRate(latest.Upload))
	setText(hTotal, formatRate(latest.Total))
	setText(hSampleInfo, fmt.Sprintf("活动接口组：%d　更新时间：%s　采样间隔：2秒", latest.ActiveCount, latest.SampledAt.Format("15:04:05")))

	if !initializationComplete {
		initializationComplete = true
		initializationTimedOut = false
		stateMu.Lock()
		active := plan.Active
		stateMu.Unlock()
		if !active {
			setStatus("状态：空闲，可启动任务")
			setDetail(fmt.Sprintf("初始化完成。当前下载 %s，上传 %s，合计 %s。GPU 数据也会每2秒刷新。", formatRate(latest.Download), formatRate(latest.Upload), formatRate(latest.Total)))
		}
		appendLog(fmt.Sprintf("初始化完成：活动接口组%d，当前合计网速%s", latest.ActiveCount, formatRate(latest.Total)))
	}
	evaluatePlan()
}

func applyGPUDisplay(latest sampleResult) {
	if len(gpuAdapters) == 0 {
		return
	}
	if latest.GPUErr != nil {
		setText(hGPUCount, fmt.Sprintf("检测到 %d 张显卡 · GPU数据读取失败", len(gpuAdapters)))
		setText(hGPUInfo, latest.GPUErr.Error())
		return
	}
	if len(latest.GPUs) == 0 {
		setText(hGPUCount, fmt.Sprintf("检测到 %d 张显卡 · 正在建立GPU基线…", len(gpuAdapters)))
		return
	}
	lines := make([]string, 0, len(latest.GPUs))
	for _, gpu := range latest.GPUs {
		util := "N/A"
		if gpu.UtilValid {
			util = fmt.Sprintf("%.1f%%", gpu.UtilPercent)
		}
		mem := "N/A"
		if gpu.MemoryValid {
			mem = fmt.Sprintf("%s / %s (%.1f%%)", formatBytes(gpu.MemoryUsed), formatBytes(gpu.MemoryTotal), gpu.MemoryPercent)
		} else if gpu.MemoryTotal > 0 {
			mem = "读取中 / " + formatBytes(gpu.MemoryTotal)
		}
		lines = append(lines, fmt.Sprintf("GPU %d · %s | 使用率 %s | 显存 %s", gpu.Index, gpu.Name, util, mem))
	}
	setText(hGPUCount, fmt.Sprintf("检测到 %d 张显卡 · 更新时间 %s", len(latest.GPUs), latest.SampledAt.Format("15:04:05")))
	setText(hGPUInfo, strings.Join(lines, "\r\n"))
}

func getIfRows() ([]mibIfRow, error) {
	size := uint32(16 * 1024)
	for attempt := 0; attempt < 4; attempt++ {
		if size < 4 {
			size = 4 + uint32(unsafe.Sizeof(mibIfRow{}))*64
		}
		buf := make([]byte, size)
		r, _, _ := procGetIfTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0)
		code := uint32(r)
		if code == ERROR_INSUFFICIENT_BUFFER || code == ERROR_BUFFER_OVERFLOW {
			continue
		}
		if code != NO_ERROR {
			return nil, fmt.Errorf("GetIfTable 错误码 %d", code)
		}
		if len(buf) < 4 {
			return nil, fmt.Errorf("GetIfTable 返回数据过短")
		}
		count := *(*uint32)(unsafe.Pointer(&buf[0]))
		rowSize := uintptr(unsafe.Sizeof(mibIfRow{}))
		required := uintptr(4) + uintptr(count)*rowSize
		if required > uintptr(len(buf)) {
			return nil, fmt.Errorf("GetIfTable 返回长度异常")
		}
		rows := make([]mibIfRow, 0, count)
		base := unsafe.Pointer(&buf[0])
		for i := uint32(0); i < count; i++ {
			rowPtr := unsafe.Add(base, uintptr(4)+uintptr(i)*rowSize)
			rows = append(rows, *(*mibIfRow)(rowPtr))
		}
		return rows, nil
	}
	return nil, fmt.Errorf("GetIfTable 缓冲区连续变化")
}

func counterDelta(old, cur uint32) uint64 {
	if cur >= old {
		return uint64(cur - old)
	}
	return uint64(math.MaxUint32-old) + uint64(cur) + 1
}
func mibDescription(row mibIfRow) string {
	n := int(row.DescrLen)
	if n > len(row.Descr) {
		n = len(row.Descr)
	}
	b := row.Descr[:n]
	for len(b) > 0 && b[len(b)-1] == 0 {
		b = b[:len(b)-1]
	}
	return string(b)
}
func formatRate(v float64) string {
	if v < 0 {
		v = 0
	}
	units := []string{"B/s", "KB/s", "MB/s", "GB/s"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	if v >= 10 {
		return fmt.Sprintf("%.1f %s", v, units[i])
	}
	return fmt.Sprintf("%.2f %s", v, units[i])
}

func startMonitoring() {
	mode := comboSelection(hMode)
	if (mode == modeAllBelow || mode == modeAllAbove) && !initializationComplete {
		messageBox(mainHwnd, "实时网速尚未初始化完成，请等待网速显示正常后再试。", "尚未就绪", MB_OK|MB_ICONWARNING)
		return
	}
	newPlan := monitorPlan{Active: true, Mode: mode}

	delay, err := strconv.Atoi(strings.TrimSpace(getText(hShutdownDelay)))
	if err != nil || delay < 0 || delay > 315360000 {
		messageBox(mainHwnd, "“关机延迟”请输入0或更大的整数秒数。", "输入有误", MB_OK|MB_ICONWARNING)
		return
	}
	newPlan.ShutdownDelaySec = delay

	switch mode {
	case modeCountdown:
		d, err := parseDurationInput(getText(hCountdown))
		if err != nil || d <= 0 {
			messageBox(mainHwnd, "倒计时格式无效。\n\n可填写01:30:00、90m、2h或3600s。", "输入有误", MB_OK|MB_ICONWARNING)
			return
		}
		newPlan.TargetTime = time.Now().Add(d)
		newPlan.Description = "倒计时关机，目标时间 " + newPlan.TargetTime.Format("2006-01-02 15:04:05")
	case modeSchedule:
		t, err := parseScheduleInput(getText(hSchedule))
		if err != nil || !t.After(time.Now()) {
			messageBox(mainHwnd, "定时时间无效，或该时间已经过去。\n\n推荐格式：2026-08-17 23:30:00。", "输入有误", MB_OK|MB_ICONWARNING)
			return
		}
		newPlan.TargetTime = t
		newPlan.Description = "定时关机，目标时间 " + t.Format("2006-01-02 15:04:05")
	case modeAllBelow, modeAllAbove:
		threshold, err := strconv.ParseFloat(strings.TrimSpace(getText(hThreshold)), 64)
		if err != nil || threshold < 0 {
			messageBox(mainHwnd, "网速阈值请输入0或更大的数字。", "输入有误", MB_OK|MB_ICONWARNING)
			return
		}
		multiplier := float64(1024)
		if comboSelection(hUnit) == 1 {
			multiplier = 1024 * 1024
		}
		newPlan.ThresholdBytesPerSec = threshold * multiplier
		stable, err := strconv.Atoi(strings.TrimSpace(getText(hStableSeconds)))
		if err != nil || stable <= 0 {
			messageBox(mainHwnd, "持续时间请输入大于0的整数秒数。", "输入有误", MB_OK|MB_ICONWARNING)
			return
		}
		newPlan.StableDuration = time.Duration(stable) * time.Second
		relation := "低于"
		if mode == modeAllAbove {
			relation = "高于"
		}
		newPlan.Description = fmt.Sprintf("全部网卡合计持续%s %s 达 %d 秒", relation, formatRate(newPlan.ThresholdBytesPerSec), stable)
	case modeGPUBelowEither:
		if len(gpuAdapters) == 0 {
			messageBox(mainHwnd, "未检测到可用于监控的显卡。", "GPU不可用", MB_OK|MB_ICONWARNING)
			return
		}
		idx := comboSelection(hGPUSelect)
		if idx < 0 || idx >= len(gpuAdapters) {
			messageBox(mainHwnd, "请选择要监控的显卡。", "输入有误", MB_OK|MB_ICONWARNING)
			return
		}
		util, err1 := strconv.ParseFloat(strings.TrimSpace(getText(hGPUUtil)), 64)
		vram, err2 := strconv.ParseFloat(strings.TrimSpace(getText(hGPUVRAM)), 64)
		if err1 != nil || err2 != nil || util < 0 || util > 100 || vram < 0 || vram > 100 {
			messageBox(mainHwnd, "GPU使用率和显存使用率阈值请输入0～100之间的数字。", "输入有误", MB_OK|MB_ICONWARNING)
			return
		}
		newPlan.GPUIndex = idx
		newPlan.GPUName = gpuAdapters[idx].Name
		newPlan.GPUUtilThreshold = util
		newPlan.GPUVRAMThreshold = vram
		newPlan.StableDuration = time.Duration(delay) * time.Second
		newPlan.ShutdownDelaySec = 0
		newPlan.Description = fmt.Sprintf("GPU %d %s：使用率 < %.1f%% 或 显存使用率 < %.1f%%，连续满足 %d 秒后关机", idx, newPlan.GPUName, util, vram, delay)
	default:
		messageBox(mainHwnd, "执行模式无效，请重新选择。", "输入有误", MB_OK|MB_ICONWARNING)
		return
	}

	stateMu.Lock()
	plan = newPlan
	stateMu.Unlock()
	saveSettings()
	enableWindow(hStart, false)
	enableWindow(hStop, true)
	setStatus("状态：任务运行中")
	setDetail("已启动：" + newPlan.Description)
	appendLog("启动任务：" + newPlan.Description)
}

func stopMonitoring(abortShutdown bool) {
	stateMu.Lock()
	wasActive := plan.Active
	plan = monitorPlan{}
	stateMu.Unlock()
	enableWindow(hStart, true)
	enableWindow(hStop, false)
	setStatus("状态：已停止")
	setDetail("监控任务已停止。")
	if abortShutdown && checkState(hAutoAbort) {
		_ = runShutdownAbort()
		appendLog("尝试取消 Windows 已排定的关机")
	}
	if wasActive {
		appendLog("任务已由用户停止")
	}
}

func evaluatePlan() {
	stateMu.Lock()
	p := plan
	stateMu.Unlock()
	if !p.Active {
		return
	}
	now := time.Now()

	if p.Mode == modeCountdown || p.Mode == modeSchedule {
		remaining := p.TargetTime.Sub(now)
		if remaining <= 0 {
			triggerShutdown("时间条件已达到")
			return
		}
		setStatus("状态：运行中 · 剩余 " + formatDuration(remaining))
		setDetail("预计关机：" + p.TargetTime.Format("2006-01-02 15:04:05"))
		return
	}

	if p.Mode == modeGPUBelowEither {
		if latestSample.GPUErr != nil || latestSample.SampledAt.IsZero() || time.Since(latestSample.SampledAt) > 8*time.Second {
			resetConditionSince()
			setStatus("状态：等待有效GPU数据，条件计时暂停")
			return
		}
		var gpu *gpuSample
		for i := range latestSample.GPUs {
			if latestSample.GPUs[i].Index == p.GPUIndex {
				gpu = &latestSample.GPUs[i]
				break
			}
		}
		if gpu == nil || (!gpu.UtilValid && !gpu.MemoryValid) {
			resetConditionSince()
			setStatus("状态：指定GPU数据暂不可用，条件计时暂停")
			return
		}
		utilLow := gpu.UtilValid && gpu.UtilPercent < p.GPUUtilThreshold
		vramLow := gpu.MemoryValid && gpu.MemoryPercent < p.GPUVRAMThreshold
		condition := utilLow || vramLow
		stateMu.Lock()
		if !plan.Active {
			stateMu.Unlock()
			return
		}
		if condition {
			if plan.ConditionSince.IsZero() {
				plan.ConditionSince = now
			}
		} else {
			plan.ConditionSince = time.Time{}
		}
		since := plan.ConditionSince
		stableDuration := plan.StableDuration
		stateMu.Unlock()

		utilText := "N/A"
		if gpu.UtilValid {
			utilText = fmt.Sprintf("%.1f%%", gpu.UtilPercent)
		}
		memText := "N/A"
		if gpu.MemoryValid {
			memText = fmt.Sprintf("%.1f%%", gpu.MemoryPercent)
		}
		if condition {
			elapsed := now.Sub(since)
			remain := stableDuration - elapsed
			if stableDuration <= 0 || remain <= 0 {
				reasons := make([]string, 0, 2)
				if utilLow {
					reasons = append(reasons, fmt.Sprintf("GPU使用率 %.1f%% < %.1f%%", gpu.UtilPercent, p.GPUUtilThreshold))
				}
				if vramLow {
					reasons = append(reasons, fmt.Sprintf("显存使用率 %.1f%% < %.1f%%", gpu.MemoryPercent, p.GPUVRAMThreshold))
				}
				triggerShutdown(fmt.Sprintf("GPU %d 条件满足：%s", p.GPUIndex, strings.Join(reasons, "；")))
				return
			}
			setStatus("状态：GPU条件满足 · 还需 " + formatDuration(remain))
			setDetail(fmt.Sprintf("GPU %d %s | 使用率 %s（阈值<%.1f%%） | 显存 %s（阈值<%.1f%%） | OR条件已持续 %s / %s", p.GPUIndex, p.GPUName, utilText, p.GPUUtilThreshold, memText, p.GPUVRAMThreshold, formatDuration(elapsed), formatDuration(stableDuration)))
		} else {
			setStatus("状态：运行中 · 等待GPU低负载条件")
			setDetail(fmt.Sprintf("GPU %d %s | 使用率 %s（需<%.1f%%） | 显存 %s（需<%.1f%%） | 两项当前都未低于阈值，延迟计时已清零", p.GPUIndex, p.GPUName, utilText, p.GPUUtilThreshold, memText, p.GPUVRAMThreshold))
		}
		return
	}

	if !latestSample.Ready || latestSample.SampledAt.IsZero() || time.Since(latestSample.SampledAt) > 8*time.Second {
		resetConditionSince()
		setStatus("状态：等待有效网速，条件计时暂停")
		return
	}

	speed := latestSample.Total
	condition := speed < p.ThresholdBytesPerSec
	relation := "低于"
	if p.Mode == modeAllAbove {
		condition = speed > p.ThresholdBytesPerSec
		relation = "高于"
	}

	stateMu.Lock()
	if !plan.Active {
		stateMu.Unlock()
		return
	}
	if condition {
		if plan.ConditionSince.IsZero() {
			plan.ConditionSince = now
		}
	} else {
		plan.ConditionSince = time.Time{}
	}
	since := plan.ConditionSince
	stableDuration := plan.StableDuration
	stateMu.Unlock()

	if condition {
		elapsed := now.Sub(since)
		remain := stableDuration - elapsed
		if remain <= 0 {
			triggerShutdown(fmt.Sprintf("总网速持续%s阈值达到%d秒", relation, int(stableDuration.Seconds())))
			return
		}
		setStatus("状态：条件满足 · 还需 " + formatDuration(remain))
		setDetail(fmt.Sprintf("当前 %s，要求%s %s；已持续 %s / %s", formatRate(speed), relation, formatRate(p.ThresholdBytesPerSec), formatDuration(elapsed), formatDuration(stableDuration)))
	} else {
		setStatus("状态：运行中 · 等待条件")
		setDetail(fmt.Sprintf("当前 %s，要求%s %s；持续计时已清零", formatRate(speed), relation, formatRate(p.ThresholdBytesPerSec)))
	}
}

func resetConditionSince() {
	stateMu.Lock()
	if plan.Active {
		plan.ConditionSince = time.Time{}
	}
	stateMu.Unlock()
}

func triggerShutdown(reason string) {
	stateMu.Lock()
	if !plan.Active {
		stateMu.Unlock()
		return
	}
	delay := plan.ShutdownDelaySec
	plan.Active = false
	stateMu.Unlock()

	appendLog("触发关机：" + reason)
	if err := runShutdown(delay, reason); err != nil {
		setStatus("状态：执行关机失败")
		setDetail("关机命令失败：" + err.Error())
		messageBox(mainHwnd, "执行 Windows 关机命令失败：\n"+err.Error(), "关机失败", MB_OK|MB_ICONERROR)
		enableWindow(hStart, true)
		enableWindow(hStop, false)
		return
	}
	setStatus(fmt.Sprintf("状态：已触发关机 · %d秒后执行", delay))
	setDetail("触发原因：" + reason + "；可点击“停止并取消”撤销。")
	enableWindow(hStart, true)
	enableWindow(hStop, true)
	messageBox(mainHwnd, fmt.Sprintf("已达到关机条件。Windows将在%d秒后关闭。\n\n点击“停止并取消”可尝试撤销。", delay), "已触发关机", MB_OK|MB_ICONINFORMATION)
}

func runShutdown(delay int, reason string) error {
	cleanReason := strings.ReplaceAll(reason, "\"", "'")
	cmd := exec.Command("shutdown.exe", "/s", "/t", strconv.Itoa(delay), "/c", "网卡智能关机助手："+cleanReason)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}
func runShutdownAbort() error {
	cmd := exec.Command("shutdown.exe", "/a")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

func parseDurationInput(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) == 2 || len(parts) == 3 {
			nums := make([]int, len(parts))
			for i, p := range parts {
				n, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil || n < 0 {
					return 0, fmt.Errorf("invalid")
				}
				nums[i] = n
			}
			h, m, sec := 0, 0, 0
			if len(nums) == 3 {
				h, m, sec = nums[0], nums[1], nums[2]
			} else {
				m, sec = nums[0], nums[1]
			}
			return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	return 0, fmt.Errorf("invalid duration")
}
func parseScheduleInput(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006/01/02 15:04:05", "2006/01/02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			now := time.Now()
			target := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local)
			if !target.After(now) {
				target = target.Add(24 * time.Hour)
			}
			return target, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid schedule")
}
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Round(time.Second).Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func setStatus(text string) { setText(hStatus, "  "+text) }
func setDetail(text string) { setText(hDetail, text) }
func appendLog(text string) {
	line := time.Now().Format("2006-01-02 15:04:05") + " | " + text + "\r\n"
	if f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
}
func openPath(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.WriteFile(path, []byte(""), 0644)
	}
	procShellExecuteW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("open"))), uintptr(unsafe.Pointer(utf16Ptr(path))), 0, 0, SW_SHOWNORMAL)
}
func saveSettings() {
	s := settings{
		Mode: comboSelection(hMode), Countdown: getText(hCountdown), Schedule: getText(hSchedule),
		Threshold: getText(hThreshold), Unit: comboSelection(hUnit), StableSeconds: getText(hStableSeconds),
		ShutdownDelay: getText(hShutdownDelay), AutoAbort: checkState(hAutoAbort), GPUIndex: comboSelection(hGPUSelect),
		GPUUtil: getText(hGPUUtil), GPUVRAM: getText(hGPUVRAM),
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(configFilePath, data, 0644)
}

func loadSettings() {
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		return
	}
	var s settings
	if json.Unmarshal(data, &s) != nil {
		return
	}
	if s.Mode >= 0 && s.Mode <= modeGPUBelowEither {
		setComboSelection(hMode, s.Mode)
	} else {
		setComboSelection(hMode, modeCountdown)
	}
	if s.Countdown != "" {
		setText(hCountdown, s.Countdown)
	}
	if s.Schedule != "" {
		setText(hSchedule, s.Schedule)
	}
	if s.Threshold != "" {
		setText(hThreshold, s.Threshold)
	}
	if s.Unit >= 0 && s.Unit <= 1 {
		setComboSelection(hUnit, s.Unit)
	}
	if s.StableSeconds != "" {
		setText(hStableSeconds, s.StableSeconds)
	}
	if s.ShutdownDelay != "" {
		setText(hShutdownDelay, s.ShutdownDelay)
	}
	if s.GPUUtil != "" {
		setText(hGPUUtil, s.GPUUtil)
	}
	if s.GPUVRAM != "" {
		setText(hGPUVRAM, s.GPUVRAM)
	}
	if s.GPUIndex >= 0 && s.GPUIndex < len(gpuAdapters) {
		setComboSelection(hGPUSelect, s.GPUIndex)
	}
	setCheck(hAutoAbort, s.AutoAbort)
}
