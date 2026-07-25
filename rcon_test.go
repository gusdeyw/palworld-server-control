package main

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestExecuteRCONHandlesTwoPacketAuthentication(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()

		auth, err := readRCONPacket(connection)
		if err != nil {
			serverDone <- err
			return
		}
		if auth.Type != rconAuth || auth.Body != "secret" {
			t.Errorf("unexpected auth packet: %#v", auth)
		}
		if err := writeRCONPacket(connection, rconPacket{ID: 1, Type: rconResponseValue}); err != nil {
			serverDone <- err
			return
		}
		if err := writeRCONPacket(connection, rconPacket{ID: 1, Type: rconAuthResponse}); err != nil {
			serverDone <- err
			return
		}

		command, err := readRCONPacket(connection)
		if err != nil {
			serverDone <- err
			return
		}
		if command.Type != rconExecCommand || command.Body != "Info" {
			t.Errorf("unexpected command packet: %#v", command)
		}
		// Palworld v1 responds with ID 0 rather than echoing the command ID.
		serverDone <- writeRCONPacket(connection, rconPacket{ID: 0, Type: rconResponseValue, Body: "Palworld test server"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := ExecuteRCON(ctx, listener.Addr().String(), "secret", "Info")
	if err != nil {
		t.Fatalf("ExecuteRCON returned an error: %v", err)
	}
	if output != "Palworld test server" {
		t.Fatalf("unexpected RCON output %q", output)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}
