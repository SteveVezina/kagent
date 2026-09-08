package driver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

type rpcFrame struct {
	raw []byte
	err error
}

type rpcClient struct {
	writer io.Writer
	frames chan rpcFrame
}

func newRPCClient(writer io.Writer, reader io.Reader, maxLineBytes int) *rpcClient {
	client := &rpcClient{writer: writer, frames: make(chan rpcFrame, 32)}
	go client.read(reader, maxLineBytes)
	return client
}

func (c *rpcClient) send(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode Pi RPC command: %w", err)
	}
	data = append(data, '\n')
	if _, err := c.writer.Write(data); err != nil {
		return fmt.Errorf("write Pi RPC command: %w", err)
	}
	return nil
}

func (c *rpcClient) read(reader io.Reader, maxLineBytes int) {
	defer close(c.frames)
	scanner := bufio.NewScanner(reader)
	if maxLineBytes <= 0 {
		maxLineBytes = 1 << 20
	}
	scanner.Buffer(make([]byte, min(maxLineBytes, 64<<10)), maxLineBytes)
	for scanner.Scan() {
		raw := append([]byte(nil), scanner.Bytes()...)
		c.frames <- rpcFrame{raw: raw}
	}
	if err := scanner.Err(); err != nil {
		c.frames <- rpcFrame{err: fmt.Errorf("read Pi RPC stream: %w", err)}
	}
}
