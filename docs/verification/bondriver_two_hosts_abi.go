//go:build ignore

// Runs two independent host processes against two simulated Sony tuner leases.
// This tests the built DLL and Windows AF_UNIX transport, not physical RF reception.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

func require(ok bool, why string) {
	if !ok {
		panic(why)
	}
}
func child(dllpath string, channel uintptr, marker byte) {
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
	require(object != 0, "CreateBonDriver failed")
	table := (*[17]uintptr)(unsafe.Pointer(*(*uintptr)(unsafe.Pointer(object))))
	call := func(slot int, args ...uintptr) uintptr {
		a := append([]uintptr{object}, args...)
		r, _, _ := syscall.SyscallN(table[slot], a...)
		return r
	}
	defer call(9)
	require(call(0) != 0, "OpenTuner failed")
	require(call(14, 0, channel) != 0, "SetChannel rejected")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		buffer := make([]byte, 188)
		size := uint32(188)
		var remain uint32
		call(7, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&remain)))
		if size > 0 {
			require(buffer[4] == marker, "host received another tuner's stream")
			fmt.Println("READY")
			bufio.NewReader(os.Stdin).ReadString('\n')
			call(1)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	panic("no data from free tuner")
}
func main() {
	if len(os.Args) > 2 && os.Args[1] == "child" {
		var channel int
		fmt.Sscan(os.Args[3], &channel)
		child(os.Args[2], uintptr(channel), byte(0x51+channel))
		return
	}
	dllpath, err := filepath.Abs(os.Args[1])
	if err != nil {
		panic(err)
	}
	socketpath := filepath.Join(os.TempDir(), fmt.Sprintf("coax-two-%d.sock", os.Getpid()))
	listener, err := net.Listen("unix", socketpath)
	if err != nil {
		panic(err)
	}
	defer os.Remove(socketpath)
	ids := []string{"cech-zd1j:p1-1.1:0", "cech-zd1j:p1-2.1:0"}
	var mu sync.Mutex
	active := map[string]bool{}
	busy := 0
	maximum := 0
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			fmt.Fprint(w, "test")
			return
		}
		if r.URL.Path == "/tuners" {
			// Deliberately stale: both appear free. The second host must handle busy
			// on A and proceed to B, without interrupting the first host's lease.
			boards := []any{}
			for _, id := range ids {
				boards = append(boards, map[string]any{"name": id, "backend": "cech-zd1j", "tuners": []any{map[string]any{"id": id, "deliveries": []string{"ISDB-T"}, "leased": false}}})
			}
			json.NewEncoder(w).Encode(boards)
			return
		}
		if r.URL.Path != "/stream" {
			http.NotFound(w, r)
			return
		}
		id := r.URL.Query().Get("tuner")
		index := -1
		for i, candidate := range ids {
			if candidate == id {
				index = i
			}
		}
		if index < 0 {
			http.Error(w, "unknown_tuner", 404)
			return
		}
		mu.Lock()
		if active[id] {
			busy++
			mu.Unlock()
			w.WriteHeader(409)
			fmt.Fprint(w, `{"code":"tuner_busy"}`)
			return
		}
		active[id] = true
		count := 0
		for _, used := range active {
			if used {
				count++
			}
		}
		if count > maximum {
			maximum = count
		}
		mu.Unlock()
		defer func() { mu.Lock(); active[id] = false; mu.Unlock() }()
		expected := []string{"473142857", "479142857"}[index]
		if r.URL.Query().Get("FREQUENCY") != expected {
			http.Error(w, "wrong frequency", 400)
			return
		}
		packet := make([]byte, 188)
		packet[0] = 0x47
		packet[4] = byte(0x51 + index)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := w.Write(packet); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
		}
	})}
	go server.Serve(listener)
	defer server.Close()
	ini := "[coax]\nHost = " + socketpath + "\nStartDaemon = no\n[Tuners]\nterrestrial = ISDBT, cech-zd1j:*\n[Spaces]\n0 = Test, terrestrial\n[Channels Test]\n1, 1, CH13, terrestrial, ISDBT, 473142857, -\n1, 2, CH14, terrestrial, ISDBT, 479142857, -\n"
	err = os.WriteFile(strings.TrimSuffix(dllpath, ".dll")+".ini", []byte(ini), 0600)
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	executable, _ := os.Executable()
	commands := []*exec.Cmd{}
	stops := []func(){}
	for channel := 0; channel < 2; channel++ {
		command := exec.CommandContext(ctx, executable, "child", dllpath, fmt.Sprint(channel))
		command.Stderr = os.Stderr
		input, _ := command.StdinPipe()
		output, _ := command.StdoutPipe()
		if err = command.Start(); err != nil {
			panic(err)
		}
		commands = append(commands, command)
		stops = append(stops, func() { fmt.Fprintln(input); input.Close() })
		line, err := bufio.NewReader(output).ReadString('\n')
		if err != nil {
			panic(err)
		}
		require(strings.TrimSpace(line) == "READY", "child did not receive")
	}
	mu.Lock()
	require(maximum == 2, "two tuner leases never overlapped")
	require(busy > 0, "busy fallback was not exercised")
	mu.Unlock()
	for _, stop := range stops {
		stop()
	}
	for _, command := range commands {
		if err = command.Wait(); err != nil {
			panic(err)
		}
	}
	fmt.Printf("PASS: two independent Windows hosts received different streams simultaneously; maximum leases=%d, busy fallback=%d\n", maximum, busy)
}
