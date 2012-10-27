/*
Example SOCKS4 proxy

Features:
	- No authentication
	- No listen port binding
	- Multiple clients AT THE SAME TIME(!)
	- Command line usage string
	- All source code in single file
*/

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

const (
	CONNECT_TIMEOUT   = 30 * time.Second
	ECHO_BUFFER_BYTES = 1024
)

const (
	COMMAND_STREAM = 0x01
	COMMAND_BIND   = 0x02
	STATUS_SUCCESS = 0x5a
	STATUS_FAILURE = 0x5b
)

type Socks4ClientRequest struct {
	Version uint8
	Command uint8
	Port    uint16
	Address [4]byte
}

type Socks4ServerResponse struct {
	Null    uint8
	Status  uint8
	Ignored [6]uint8
}

// read until a NUL byte is encountered, throwing away any bytes up to and
// including the NUL
func readUntilNul(conn net.Conn) error {
	char_buffer := [1]byte{1}
	for char_buffer[0] != 0 {
		n, err := conn.Read(char_buffer[0:])
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("Read zero bytes from connection")
		}
	}
	return nil
}

// copy bytes from the first connection to the second
func echoLoop(to_read net.Conn, to_write net.Conn, wg *sync.WaitGroup) {
	var buffer [ECHO_BUFFER_BYTES]byte
	for {
		n, err := to_read.Read(buffer[0:])
		if err != nil {
			break
		}
		_, err = to_write.Write(buffer[0:n])
		if err != nil {
			break
		}
	}
	to_write.Close()
	wg.Done()
}

func sendSocksResponse(conn net.Conn, status uint8) {
	response := Socks4ServerResponse{Status: status}
	binary.Write(conn, binary.BigEndian, &response)
}

func handleConnection(client net.Conn) error {
	fmt.Printf("Received connection from %s on %s\n", client.RemoteAddr(), client.LocalAddr())
	defer client.Close()

	// read the SOCKS connection info
	request := Socks4ClientRequest{}
	if err := binary.Read(client, binary.BigEndian, &request); err != nil {
		return err
	}

	// the request is followed by a username that we ignore, so read to the next
	// NUL byte and throw away whatever we read
	if err := readUntilNul(client); err != nil {
		return err
	}

	// no binding allowed
	if request.Command != COMMAND_STREAM {
		sendSocksResponse(client, STATUS_FAILURE)
		return errors.New(fmt.Sprint("Unsupported command:", request.Command))
	}

	server_ip := net.IP(request.Address[0:])
	server_addr := fmt.Sprint(server_ip, ":", request.Port)

	server, err := net.DialTimeout("tcp", server_addr, CONNECT_TIMEOUT)
	if err != nil {
		sendSocksResponse(client, STATUS_FAILURE)
		return errors.New(fmt.Sprint("Failed to connect to remote host:", server_addr, err))
	}
	defer server.Close()

	sendSocksResponse(client, STATUS_SUCCESS)

	var wg sync.WaitGroup
	wg.Add(2)
	go echoLoop(server, client, &wg)
	go echoLoop(client, server, &wg)
	wg.Wait()
	fmt.Printf("Closed connection from %s on %s\n", client.RemoteAddr(), client.LocalAddr())
	return nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage:", os.Args[0], "<listen ip:port>")
		os.Exit(1)
	}
	listen_address := os.Args[1]

	listener, err := net.Listen("tcp", listen_address)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not listen on", listen_address)
		os.Exit(1)
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error from listen port:", err)
			os.Exit(1)
		}
		go func() {
			if err := handleConnection(conn); err != nil {
				fmt.Println("Error:", err)
			}
		}()
	}
}
