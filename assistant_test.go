package p2p

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	_ "net/http/pprof"

	"github.com/codingeasygo/util/xio"
	"github.com/codingeasygo/util/xio/frame"
	"github.com/codingeasygo/util/xmap"

	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetLevel(log.DebugLevel)
	log.SetFormatter(NewPlainFormatter())
	go func() {
		http.ListenAndServe(":6060", nil)
	}()
}

func TestAssistantConn(t *testing.T) {
	server := NewAssisServer(AssisVerifierF(func(conn *AssisConn, options xmap.M) (attrs xmap.M, err error) {
		fmt.Printf("verify local %v\n", conn.LocalAddr())
		attrs = options
		return
	}))
	err := server.Start("tcp://:14322,udp://:14322")
	if err != nil {
		t.Error(err)
		return
	}
	//tcp
	go func() {
		remote, err := AssisPunching("tcp://127.0.0.1:14322?uuid=10&waiting=11")
		if err != nil {
			t.Error(err)
			return
		}
		fmt.Println(remote)
	}()
	remote, err := AssisPunching("tcp://127.0.0.1:14322?uuid=11&waiting=10")
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Println(remote)
	//
	//udp
	go func() {
		remote, err := AssisPunching("udp://127.0.0.1:14322?uuid=20&waiting=21")
		if err != nil {
			t.Error(err)
			return
		}
		fmt.Println(remote)
	}()
	remote, err = AssisPunching("udp://127.0.0.1:14322?uuid=21&waiting=20")
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Println(remote)
	//
	//self
	remote, err = AssisPunching("tcp://127.0.0.1:14322?uuid=100&waiting=100")
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Println(remote)
	remote, err = AssisPunching("udp://127.0.0.1:14322?uuid=100&waiting=100")
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Println(remote)
	//
	server.Stop()
}

func TestAssisTimeout(t *testing.T) {
	server := NewAssisServer(NewAssisAccessAll())
	server.delay = 10 * time.Millisecond
	server.StartTimeout()
	server.StartTimeout()
	err := server.Start("tcp://:14322?timeout=1,udp://:14322?timeout=1")
	if err != nil {
		t.Error(err)
		return
	}
	//wait timeout
	time.Sleep(1000 * time.Millisecond)
	_, err = AssisPunching("tcp://127.0.0.1:14322?uuid=11&waiting=10")
	if err == nil {
		t.Error(err)
		return
	}
	//
	server.Stop()
}

func TestAssisStop(t *testing.T) {
	server := NewAssisServer(NewAssisAccessAll())
	err := server.Start("tcp://:14322")
	if err != nil {
		t.Error(err)
		return
	}
	//wait timeout
	go func() {
		_, err = AssisPunching("tcp://127.0.0.1:14322?uuid=11&waiting=10")
		if err == nil {
			t.Error(err)
			return
		}
		server.Stop()
	}()
	time.Sleep(200 * time.Millisecond)
	_, err = AssisPunching("tcp://127.0.0.1:14322?uuid=11&waiting=10")
	if err == nil {
		t.Error(err)
		return
	}
}

func TestAssisError(t *testing.T) {
	server := NewAssisServer(AssisVerifierF(func(conn *AssisConn, options xmap.M) (attrs xmap.M, err error) {
		if len(options.Str("error")) > 0 {
			err = fmt.Errorf("not uuid")
		}
		attrs = options
		return
	}))
	err := server.Start("tcp://:14322,udp://:14322")
	if err != nil {
		t.Error(err)
		return
	}
	defer server.Stop()
	_, err = AssisPunching("tcp://127.0.0.1:14322")
	if err == nil {
		t.Error(err)
		return
	}
	_, err = AssisPunching("tcp://127.0.0.1:14322?uuid=11&waiting=10&error=1")
	if err == nil {
		t.Error(err)
		return
	}
	conn, err := net.Dial("tcp", "127.0.0.1:14322")
	if err != nil {
		t.Error(err)
		return
	}
	conn.Write(frame.Wrap([]byte("123")))
	conn.Read(make([]byte, 1024))
	//
	err = server.Start("tcp://\x01:14322")
	if err == nil {
		t.Error(err)
		return
	}
	//
	err = server.Start("tcp://:3234324")
	if err == nil {
		t.Error(err)
		return
	}
	//
	err = server.Start("udp://:3234324")
	if err == nil {
		t.Error(err)
		return
	}
	//
	_, err = AssisPunching("\x01")
	if err == nil {
		t.Error(err)
		return
	}
	//
	_, err = AssisPunching("tcp://127.0.0.1:382")
	if err == nil {
		t.Error(err)
		return
	}
	//
	_, err = AssisNetworkPunching("tcp", "", "127.0.0.1:14322", 1024, time.Second, xmap.M{"xx": TestAssisError})
	if err == nil {
		t.Error(err)
		return
	}
	//
	go func() {
		buf := make([]byte, 1024)
		packet, _ := net.ListenPacket("udp", ":15522")
		_, from, _ := packet.ReadFrom(buf)
		packet.WriteTo(frame.Wrap([]byte("1000")), from)
	}()
	time.Sleep(10 * time.Millisecond)
	_, err = AssisPunching("udp://127.0.0.1:15522")
	if err == nil {
		t.Error(err)
		return
	}
}

func TestDialTCP(t *testing.T) {
	server := NewAssisServer(AssisVerifierF(func(conn *AssisConn, options xmap.M) (attrs xmap.M, err error) {
		// fmt.Printf("verify local %v\n", conn.LocalAddr())
		attrs = options
		return
	}))
	err := server.Start("tcp://:14322,udp://:14322")
	if err != nil {
		t.Error(err)
		return
	}
	//tcp
	var remote net.Conn
	go func() {
		conn, _, err := Dial("tcp://127.0.0.1:14322?local=11&&remote=10&listen=1")
		if err != nil {
			t.Error(err)
			return
		}
		remote = conn
		io.Copy(conn, conn)
	}()
	local, _, err := Dial("tcp://127.0.0.1:14322?local=10&&remote=11&listen=0")
	if err != nil {
		t.Error(err)
		return
	}
	_, err = fmt.Fprintf(local, "testing->0")
	if err != nil {
		t.Error(err)
		return
	}
	buffer := make([]byte, 1024)
	n, err := local.Read(buffer)
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Println(string(buffer[0:n]))
	remote.Close()
	_, err = local.Read(buffer)
	if err == nil {
		t.Error(err)
		return
	}
	//
	server.Stop()
}

func TestDialUDP(t *testing.T) {
	server := NewAssisServer(AssisVerifierF(func(conn *AssisConn, options xmap.M) (attrs xmap.M, err error) {
		// fmt.Printf("verify local %v\n", conn.LocalAddr())
		attrs = options
		return
	}))
	err := server.Start("tcp://:14322,udp://:14322")
	if err != nil {
		t.Error(err)
		return
	}
	//tcp
	var remote net.Conn
	go func() {
		conn, _, err := Dial("udp://127.0.0.1:14322?local=11&&remote=10")
		if err != nil {
			t.Error(err)
			return
		}
		remote = conn
		xio.CopyPacketConn(conn, conn.(*net.UDPConn))
	}()
	local, to, err := Dial("udp://127.0.0.1:14322?local=10&&remote=11&listen=0")
	if err != nil {
		t.Error(err)
		return
	}
	_, err = local.(*net.UDPConn).WriteTo([]byte("testing->0"), to)
	if err != nil {
		t.Error(err)
		return
	}
	buffer := make([]byte, 1024)
	n, _, err := local.(*net.UDPConn).ReadFrom(buffer)
	if err != nil {
		t.Error(err)
		return
	}
	fmt.Println(string(buffer[0:n]))
	remote.Close()
	local.Close()
	_, _, err = local.(*net.UDPConn).ReadFrom(buffer)
	if err == nil {
		t.Error(err)
		return
	}
	//
	server.Stop()
}
