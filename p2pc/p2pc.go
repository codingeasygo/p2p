package main

import (
	"io"
	"net"
	"os"

	"github.com/codingeasygo/p2p"
	"github.com/codingeasygo/util/xio"
	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetLevel(log.DebugLevel)
	switch os.Args[1] {
	case "assis":
		server := p2p.NewAssisServer(p2p.NewAssisAccessAll())
		err := server.Start(os.Args[2])
		if err != nil {
			log.Errorf("p2ptest assistant server start by %v fail with %v", os.Args[2], err)
			os.Exit(1)
		}
		waiter := make(chan int)
		waiter <- 1
	case "peer":
		conn, to, err := p2p.Dial(os.Args[3])
		if err != nil {
			log.Errorf("p2ptest dial peer by %v fail with %v", os.Args[3], err)
			os.Exit(1)
		}
		if os.Args[2] == "echo" {
			if udp, ok := conn.(*net.UDPConn); ok {
				xio.CopyPacketConn(udp, udp)
			} else {
				io.Copy(conn, conn)
			}
		} else {
			if udp, ok := conn.(*net.UDPConn); ok {
				go xio.CopyPacketConn(os.Stdout, udp)
				xio.CopyTo(udp, to, os.Stdin)
			} else {
				go io.Copy(os.Stdout, conn)
				io.Copy(conn, os.Stdin)
			}
		}
	}
}
