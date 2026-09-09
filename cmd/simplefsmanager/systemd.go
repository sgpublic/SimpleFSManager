package main

import (
	"net"
	"os"
)

// notifySystemd reports service state through the socket systemd provides.
// It is a no-op outside a systemd service environment.
func notifySystemd(state string) error {
	socket := os.Getenv("NOTIFY_SOCKET")
	if socket == "" {
		return nil
	}
	connection, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer connection.Close()
	_, err = connection.Write([]byte(state))
	return err
}
