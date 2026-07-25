package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	rconAuth          int32 = 3
	rconExecCommand   int32 = 2
	rconAuthResponse  int32 = 2
	rconResponseValue int32 = 0
)

type rconPacket struct {
	ID   int32
	Type int32
	Body string
}

func ExecuteRCON(ctx context.Context, addr, password, command string) (string, error) {
	dialer := net.Dialer{Timeout: 4 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("connect to RCON: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))

	if err := writeRCONPacket(conn, rconPacket{ID: 1, Type: rconAuth, Body: password}); err != nil {
		return "", fmt.Errorf("authenticate: %w", err)
	}
	authenticated := false
	for attempts := 0; attempts < 3; attempts++ {
		auth, err := readRCONPacket(conn)
		if err != nil {
			return "", fmt.Errorf("authenticate: %w", err)
		}
		if auth.ID == -1 {
			return "", errors.New("RCON authentication failed")
		}
		if auth.Type == rconAuthResponse && auth.ID == 1 {
			authenticated = true
			break
		}
	}
	if !authenticated {
		return "", errors.New("RCON authentication response was not received")
	}

	if err := writeRCONPacket(conn, rconPacket{ID: 2, Type: rconExecCommand, Body: command}); err != nil {
		return "", fmt.Errorf("send command: %w", err)
	}
	for attempts := 0; attempts < 3; attempts++ {
		response, err := readRCONPacket(conn)
		if err != nil {
			return "", fmt.Errorf("read command response: %w", err)
		}
		// Current Palworld builds reply to commands with packet ID 0 even when
		// the request used a different ID. Match the response type after a
		// successful authentication instead of requiring the echoed ID.
		if response.ID >= 0 && response.Type == rconResponseValue {
			return strings.TrimSpace(response.Body), nil
		}
	}
	return "", errors.New("RCON command response was not received")
}

func writeRCONPacket(writer io.Writer, packet rconPacket) error {
	body := []byte(packet.Body)
	size := int32(4 + 4 + len(body) + 2)
	buffer := make([]byte, 4+size)
	binary.LittleEndian.PutUint32(buffer[0:4], uint32(size))
	binary.LittleEndian.PutUint32(buffer[4:8], uint32(packet.ID))
	binary.LittleEndian.PutUint32(buffer[8:12], uint32(packet.Type))
	copy(buffer[12:], body)
	buffer[len(buffer)-2] = 0
	buffer[len(buffer)-1] = 0
	_, err := writer.Write(buffer)
	return err
}

func readRCONPacket(reader io.Reader) (rconPacket, error) {
	var size int32
	if err := binary.Read(reader, binary.LittleEndian, &size); err != nil {
		return rconPacket{}, err
	}
	if size < 10 || size > 4*1024*1024 {
		return rconPacket{}, fmt.Errorf("invalid RCON packet size %d", size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		return rconPacket{}, err
	}
	id := int32(binary.LittleEndian.Uint32(data[0:4]))
	packetType := int32(binary.LittleEndian.Uint32(data[4:8]))
	body := data[8 : len(data)-2]
	return rconPacket{ID: id, Type: packetType, Body: string(body)}, nil
}
