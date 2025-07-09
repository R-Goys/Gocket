package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		panic(err)
	}
	go func() {
		syscall.Bind(fd, &syscall.SockaddrInet4{
			Port: 8080,
			Addr: [4]byte{127, 0, 0, 1},
		})
		err = syscall.Listen(fd, 5)
		if err != nil {
			log.Panic("Listen Error: ", err)
			return
		}
		for {
			connFd, sa, err := syscall.Accept(fd)
			if err != nil {
				log.Panic("Accept Error: ", err)
				continue
			}
			fmt.Println("Accepted connection from:", sa)
			defer syscall.Close(connFd)
			buf := make([]byte, 1024)
			n, err := syscall.Read(connFd, buf)
			if err != nil {
				log.Panic("Read Error: ", err)
				return
			}
			fmt.Printf("Received from client: %s\n", string(buf[:n]))
			resp := "Hello! You sent: " + string(buf[:n])
			_, err = syscall.Write(connFd, []byte(resp))
			if err != nil {
				log.Panic("Write Error: ", err)
				return
			}
			syscall.Close(connFd)
		}
	}()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	syscall.Close(fd)

}
