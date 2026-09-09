package main

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestNotifySystemd(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "notify.sock")
	listener, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("NOTIFY_SOCKET", socket)

	if err := notifySystemd("READY=1"); err != nil {
		t.Fatal(err)
	}
	if err := listener.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 64)
	count, _, err := listener.ReadFromUnix(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if received := string(buffer[:count]); received != "READY=1" {
		t.Fatalf("notification = %q, want READY=1", received)
	}
}

func TestNotifySystemdWithoutSocket(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	if err := notifySystemd("READY=1"); err != nil {
		t.Fatal(err)
	}
}
