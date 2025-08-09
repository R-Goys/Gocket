package main

import (
	"log"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
)

type Conn struct {
	fd     int
	events uint32
	out    []byte
}

func must(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}

func mod(epfd int, c *Conn) {
	ev := &unix.EpollEvent{Events: c.events, Fd: int32(c.fd)}
	_ = unix.EpollCtl(epfd, unix.EPOLL_CTL_MOD, c.fd, ev)
}

func enableWrite(epfd int, c *Conn) {
	if (c.events & unix.EPOLLOUT) == 0 {
		c.events |= unix.EPOLLOUT
		mod(epfd, c)
	}
}

func disableWrite(epfd int, c *Conn) {
	if (c.events & unix.EPOLLOUT) != 0 {
		c.events &^= unix.EPOLLOUT
		mod(epfd, c)
	}
}

func main() {
	// 监听 socket（非阻塞）
	lfd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_NONBLOCK, 0)
	must(err, "socket")
	must(unix.SetsockoptInt(lfd, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1), "SO_REUSEADDR")
	addr := &unix.SockaddrInet4{Port: 8080, Addr: [4]byte{127, 0, 0, 1}}
	// 绑定监听地址
	must(unix.Bind(lfd, addr), "bind")
	// 监听
	must(unix.Listen(lfd, 128), "listen")

	// epoll
	epfd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	must(err, "epoll_create1")
	defer unix.Close(epfd)

	// LT 模式
	must(unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, lfd,
		&unix.EpollEvent{Events: unix.EPOLLIN, Fd: int32(lfd)}), "epoll_ctl ADD listen")

	// 退出时关闭文件描述符
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	go func() {
		<-sig
		unix.Close(lfd)
		unix.Close(epfd)
		os.Exit(0)
	}()

	conns := make(map[int]*Conn)
	events := make([]unix.EpollEvent, 128)
	buf := make([]byte, 64*1024)

	for {
		n, err := unix.EpollWait(epfd, events, -1)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			log.Fatalf("epoll_wait: %v", err)
		}

		for i := 0; i < n; i++ {
			ev := events[i]

			// 监听 fd：accept 到 EAGAIN
			if int(ev.Fd) == lfd {
				for {
					cfd, sa, aerr := unix.Accept4(lfd, unix.SOCK_NONBLOCK)
					if aerr != nil {
						if aerr == unix.EAGAIN || aerr == unix.EWOULDBLOCK {
							break
						}
						log.Printf("accept: %v", aerr)
						break
					}
					log.Println("accept:", sa)
					// 加入监听事件
					c := &Conn{fd: cfd, events: unix.EPOLLIN}
					conns[cfd] = c
					_ = unix.EpollCtl(epfd, unix.EPOLL_CTL_ADD, cfd,
						&unix.EpollEvent{Events: c.events, Fd: int32(cfd)})
				}
				continue
			}

			fd := int(ev.Fd)

			// 错误/挂断
			if (ev.Events & (unix.EPOLLERR | unix.EPOLLHUP)) != 0 {
				unix.EpollCtl(epfd, unix.EPOLL_CTL_DEL, fd, nil)
				unix.Close(fd)
				delete(conns, fd)
				continue
			}

			// 到了这里，如果触发的文件描述符存在于维护的哈希表里面，说明我们接受的连接可读。
			c := conns[fd]
			if c == nil {
				continue
			}

			// 可读：读到 EAGAIN，把要发的数据放入 out，然后打开 EPOLLOUT
			if (ev.Events & unix.EPOLLIN) != 0 {
				var closed bool
				for {
					nr, rerr := unix.Read(fd, buf)
					if nr > 0 {
						c.out = append(c.out, []byte("Hello! You sent: ")...)
						c.out = append(c.out, buf[:nr]...)
						continue
					}
					if rerr == nil && nr == 0 {
						closed = true
						break
					}
					if rerr == unix.EAGAIN || rerr == unix.EWOULDBLOCK {
						break
					}
					if rerr == unix.EINTR {
						continue
					}
					log.Printf("read(%d): %v", fd, rerr)
					closed = true
					break
				}
				if closed {
					unix.EpollCtl(epfd, unix.EPOLL_CTL_DEL, fd, nil)
					unix.Close(fd)
					delete(conns, fd)
					continue
				}
				if len(c.out) > 0 {
					enableWrite(epfd, c)
				}
			}

			// 可写：尽量写空；写空后关闭 EPOLLOUT
			if (ev.Events & unix.EPOLLOUT) != 0 {
				for len(c.out) > 0 {
					nw, werr := unix.Write(fd, c.out)
					if nw > 0 {
						c.out = c.out[nw:]
						continue
					}
					if werr == unix.EAGAIN || werr == unix.EWOULDBLOCK {
						break
					}
					if werr == unix.EINTR {
						continue
					}
					log.Printf("write(%d): %v", fd, werr)
					unix.EpollCtl(epfd, unix.EPOLL_CTL_DEL, fd, nil)
					unix.Close(fd)
					delete(conns, fd)
					break
				}
				if len(c.out) == 0 {
					disableWrite(epfd, c)
				}
			}
		}
	}
}
