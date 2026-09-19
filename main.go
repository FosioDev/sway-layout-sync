package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ipcMagic = []byte("i3-ipc")

func findSwaySocket() string {
	sock := os.Getenv("SWAYSOCK")
	if sock != "" {
		return sock
	}
	// Fallback lookup: sway-ipc.<uid>.<pid>.sock
	matches, _ := filepath.Glob(fmt.Sprintf("/run/user/%d/sway-ipc.*.sock", os.Getuid()))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

func sendIpcMessage(conn net.Conn, msgType uint32, payload string) error {
	pBytes := []byte(payload)
	buf := make([]byte, 14+len(pBytes))
	copy(buf[0:6], ipcMagic)
	binary.LittleEndian.PutUint32(buf[6:10], uint32(len(pBytes)))
	binary.LittleEndian.PutUint32(buf[10:14], msgType)
	copy(buf[14:], pBytes)
	_, err := conn.Write(buf)
	return err
}

func readIpcMessage(conn net.Conn) (uint32, []byte, error) {
	header := make([]byte, 14)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, nil, err
	}
	length := binary.LittleEndian.Uint32(header[6:10])
	msgType := binary.LittleEndian.Uint32(header[10:14])
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return 0, nil, err
	}
	return msgType, payload, nil
}

type SwayInputEvent struct {
	Change string `json:"change"`
	Input  struct {
		ActiveLayoutIndex *int `json:"xkb_active_layout_index"`
	} `json:"input"`
}

// -------------------------------------------------------------
// SERVER MODE (Runs on HOST)
// -------------------------------------------------------------
type Server struct {
	mu            sync.Mutex
	clients       map[net.Conn]struct{}
	currentLayout int
}

func runServer(addr string) {
	swaySock := findSwaySocket()
	if swaySock == "" {
		fmt.Println("[Host Error] Sway IPC socket not found!")
		return
	}
	fmt.Printf("[Host] Connecting to local Sway IPC: %s\n", swaySock)

	ipcConn, err := net.Dial("unix", swaySock)
	if err != nil {
		fmt.Printf("[Host Error] IPC connection failed: %v\n", err)
		return
	}
	defer ipcConn.Close()

	l, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Printf("[Host Error] Failed to bind server on %s: %v\n", addr, err)
		return
	}
	defer l.Close()
	fmt.Printf("[Host] Server listening on %s. Waiting for clients...\n", addr)

	srv := &Server{
		clients: make(map[net.Conn]struct{}),
	}

	// Task 1: Accept incoming client connections
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				continue
			}
			fmt.Printf("[Host] Client connected: %s\n", conn.RemoteAddr())

			srv.mu.Lock()
			srv.clients[conn] = struct{}{}
			// Send current active layout to the newly connected client
			_, _ = fmt.Fprintf(conn, "%d\n", srv.currentLayout)
			srv.mu.Unlock()

			// Watch for client disconnection
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1)
				for {
					if _, err := c.Read(buf); err != nil {
						break
					}
				}
				srv.mu.Lock()
				delete(srv.clients, c)
				srv.mu.Unlock()
				fmt.Printf("[Host] Client disconnected: %s\n", c.RemoteAddr())
			}(conn)
		}
	}()

	// Task 2: Subscribe to Sway layout events and broadcast
	if err := sendIpcMessage(ipcConn, 2, `["input"]`); err != nil {
		fmt.Printf("[Host Error] Failed to subscribe to input events: %v\n", err)
		return
	}
	_, _, _ = readIpcMessage(ipcConn)
	fmt.Println("[Host] Subscribed to Sway input events...")

	for {
		_, payload, err := readIpcMessage(ipcConn)
		if err != nil {
			break
		}

		var ev SwayInputEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			continue
		}

		if ev.Change == "xkb_layout" && ev.Input.ActiveLayoutIndex != nil {
			idx := *ev.Input.ActiveLayoutIndex

			srv.mu.Lock()
			srv.currentLayout = idx
			fmt.Printf("[Host] Layout switched to index %d. Broadcasting...\n", idx)

			for client := range srv.clients {
				_, _ = fmt.Fprintf(client, "%d\n", idx)
			}
			srv.mu.Unlock()
		}
	}
}

// -------------------------------------------------------------
// CLIENT MODE (Runs on GUEST)
// -------------------------------------------------------------
func runClient(addr string) {
	swaySock := findSwaySocket()
	if swaySock == "" {
		fmt.Println("[Guest Error] Sway IPC socket not found! Is Sway running?")
		return
	}
	fmt.Printf("[Guest] Connecting to local Sway IPC: %s\n", swaySock)

	ipcConn, err := net.Dial("unix", swaySock)
	if err != nil {
		fmt.Printf("[Guest Error] IPC connection failed: %v\n", err)
		return
	}
	defer ipcConn.Close()

	// Reconnection loop
	for {
		fmt.Printf("[Guest] Connecting to host at %s...\n", addr)
		tcpConn, err := net.Dial("tcp", addr)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		fmt.Println("[Guest] Successfully connected to host!")

		scanner := bufio.NewScanner(tcpConn)
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				continue
			}
			idx, err := strconv.Atoi(text)
			if err != nil {
				continue
			}

			cmd := fmt.Sprintf("input type:keyboard xkb_switch_layout %d", idx)
			if err := sendIpcMessage(ipcConn, 0, cmd); err == nil {
				_, resp, _ := readIpcMessage(ipcConn)
				fmt.Printf("[Guest] Applied layout index %d: %s\n", idx, strings.TrimSpace(string(resp)))
			}
		}

		fmt.Println("[Guest] Connection lost. Reconnecting...")
		tcpConn.Close()
		time.Sleep(1 * time.Second)
	}
}

func main() {
	serverMode := flag.Bool("s", false, "Start in server mode (run on host)")
	flag.Parse()

	addr := flag.Arg(0)
	if addr == "" {
		addr = "192.168.122.1:8889"
	}

	if *serverMode {
		runServer(addr)
	} else {
		runClient(addr)
	}
}
