//go:build ignore

// Windows amd64 ABI smoke test with a real Winsock AF_UNIX connection.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

func check(ok bool, why string) {
	if !ok {
		panic(why)
	}
}
func main() {
	dllpath, _ := filepath.Abs(os.Args[1])
	socketpath := filepath.Join(os.TempDir(), fmt.Sprintf("coax-weak-%d.sock", os.Getpid()))
	listener, err := net.Listen("unix", socketpath)
	if err != nil {
		panic(err)
	}
	defer os.Remove(socketpath)
	entered := make(chan struct{}, 1)
	var requests atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			fmt.Fprint(w, "test")
			return
		}
		if r.URL.Path != "/stream" {
			http.Error(w, "unexpected", 404)
			return
		}
		n := requests.Add(1)
		if n == 1 {
			entered <- struct{}{}
			<-r.Context().Done()
			return
		}
		check(r.URL.Query().Get("FREQUENCY") == "479142857", "wrong replacement channel")
		packet := make([]byte, 188)
		packet[0] = 0x47
		packet[4] = 0x55
		w.Header().Set("Content-Type", "video/mp2t")
		w.Write(packet)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})}
	go server.Serve(listener)
	defer server.Close()
	ini := "[coax]\nHost = " + socketpath + "\nStartDaemon = no\n[Spaces]\n0 = Test\n[Channels Test]\n1, 1, CH13, -, ISDBT, 473142857, -\n1, 2, CH14, -, ISDBT, 479142857, -\n"
	err = os.WriteFile(dllpath[:len(dllpath)-4]+".ini", []byte(ini), 0600)
	if err != nil {
		panic(err)
	}
	dll, err := syscall.LoadDLL(dllpath)
	if err != nil {
		panic(err)
	}
	defer dll.Release()
	create, err := dll.FindProc("CreateBonDriver")
	if err != nil {
		panic(err)
	}
	object, _, _ := create.Call()
	check(object != 0, "CreateBonDriver failed")
	table := (*[17]uintptr)(unsafe.Pointer(*(*uintptr)(unsafe.Pointer(object))))
	call := func(slot int, args ...uintptr) uintptr {
		a := append([]uintptr{object}, args...)
		r, _, _ := syscall.SyscallN(table[slot], a...)
		return r
	}
	check(call(0) != 0, "OpenTuner failed")
	started := time.Now()
	check(call(14, 0, 0) != 0, "SetChannel rejected")
	selection := time.Since(started)
	check(selection < 200*time.Millisecond, "SetChannel blocked")
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		panic("no acquisition")
	}
	started = time.Now()
	check(call(16) == 0, "wrong channel")
	call(5)
	metadata := time.Since(started)
	check(metadata < 200*time.Millisecond, "status blocked")
	started = time.Now()
	check(call(14, 0, 1) != 0, "replacement rejected")
	replacement := time.Since(started)
	deadline := time.Now().Add(3 * time.Second)
	got := false
	for time.Now().Before(deadline) {
		buffer := make([]byte, 188)
		size := uint32(len(buffer))
		var remain uint32
		call(7, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&remain)))
		if size > 0 {
			check(size == 188 && buffer[4] == 0x55, "stale or invalid stream")
			got = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	check(got, "replacement did not recover")
	check(call(16) == 1, "stale channel restored")
	started = time.Now()
	call(1)
	closeTime := time.Since(started)
	check(closeTime < 200*time.Millisecond, "CloseTuner blocked")
	started = time.Now()
	call(9)
	releaseTime := time.Since(started)
	check(releaseTime < time.Second, "Release blocked")
	fmt.Printf("PASS actual DLL / Windows AF_UNIX: SetChannel=%v status=%v replacement=%v CloseTuner=%v Release=%v streams=%d\n", selection, metadata, replacement, closeTime, releaseTime, requests.Load())
}
