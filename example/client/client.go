package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		log.Panic("Socket Error: ", err)
	}
	syscall.Connect(fd, &syscall.SockaddrInet4{
		Port: 8080,
		Addr: [4]byte{127, 0, 0, 1},
	})
	message := "Hello, Server!"
	_, err = syscall.Write(fd, []byte(message))
	if err != nil {
		log.Panic("Write Error: ", err)
		return
	}
	buf := make([]byte, 1024)
	n, err := syscall.Read(fd, buf)
	if err != nil {
		log.Panic("Read Error: ", err)
		return
	}
	log.Printf("Received from server: %s", string(buf[:n]))
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	syscall.Close(fd)
}
