package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/codingeasygo/p2p"
	"github.com/codingeasygo/util/xio"
	log "github.com/sirupsen/logrus"
)

func usage() {
	fmt.Printf(`Usage: p2pc assis|peer <options>

Assistant Server
	p2pc assis <listen uri>
		listen url is tcp://<host>:<port> or udp://<host>:<port>

Peer Connection
	p2pc peer echo|console <connect uri>
		tcp connect is tcp://<assistant server>:<assistant server port>?local=<local peer id>&remote=<remote peer id>&<other extern arguments>
		udp connect is udp://<assistant server>:<assistant server port>?local=<local peer id>&remote=<remote peer id>&<other extern arguments>
	
`)
	os.Exit(1)
}

func main() {
	log.SetLevel(log.DebugLevel)
	if len(os.Args) < 3 {
		usage()
		return
	}
	switch os.Args[1] {
	case "assis":
		server := p2p.NewAssisServer(p2p.NewAssisAccessAll())
		err := server.Start(os.Args[2])
		if err != nil {
			log.Errorf("p2pc assistant server start by %v fail with %v", os.Args[2], err)
			os.Exit(1)
		}
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		server.Stop()
	case "peer":
		if len(os.Args) < 4 {
			usage()
			return
		}
		conn, to, err := p2p.Dial(os.Args[3])
		if err != nil {
			log.Errorf("p2pc dial peer by %v fail with %v", os.Args[3], err)
			os.Exit(1)
		}
		log.Infof("p2p peer is connected to %v", to)
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-c
			conn.Close()
			os.Stdin.Close()
		}()
		switch os.Args[2] {
		case "echo":
			log.Infof("p2p peer is starting echo server")
			if udp, ok := conn.(*net.UDPConn); ok {
				xio.CopyPacketConn(udp, udp)
			} else {
				io.Copy(conn, conn)
			}
		case "console":
			log.Infof("p2p peer is starting telnet console")
			if udp, ok := conn.(*net.UDPConn); ok {
				go xio.CopyPacketTo(udp, to, os.Stdin)
				_, err = xio.CopyPacketConn(os.Stdout, udp)
				log.Infof("p2p peer is stop by %v", err)
				os.Stdin.Close()
			} else {
				go io.Copy(conn, os.Stdin)
				_, err = io.Copy(os.Stdout, conn)
				log.Infof("p2p peer is stop by %v", err)
				os.Stdin.Close()
			}
		default:
			fmt.Printf("unkonw command %v\n", os.Args[2])
			usage()
		}
	default:
		fmt.Printf("unkonw command %v\n", os.Args[1])
		usage()
	}
}
