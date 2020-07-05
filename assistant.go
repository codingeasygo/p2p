package p2p

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/codingeasygo/util/attrvalid"
	"github.com/codingeasygo/util/converter"
	"github.com/codingeasygo/util/uuid"
	"github.com/codingeasygo/util/xio"
	"github.com/codingeasygo/util/xio/frame"
	"github.com/codingeasygo/util/xmap"
	log "github.com/sirupsen/logrus"
)

//AssisConn is assistant connection
type AssisConn struct {
	xmap.M
	UUID      string
	Waiting   string
	Type      string
	startTime time.Time
	timeout   time.Duration
	server    *AssisServer
	//tcp
	tcp net.Conn
	rwc frame.ReadWriteCloser
	//udp
	udp  net.PacketConn
	from net.Addr
}

func (a *AssisConn) Write(p []byte) (n int, err error) {
	if a.tcp == nil {
		n, err = a.udp.WriteTo(frame.Wrap(p), a.from)
	} else {
		n, err = a.rwc.Write(p)
	}
	return
}

//Close will close raw connection
func (a *AssisConn) Close() (err error) {
	if a.tcp != nil {
		err = a.tcp.Close()
	}
	return
}

//RemoteAddr will return remote address
func (a *AssisConn) RemoteAddr() net.Addr {
	if a.tcp != nil {
		return a.tcp.RemoteAddr()
	}
	return a.from
}

//LocalAddr will return local address
func (a *AssisConn) LocalAddr() net.Addr {
	if a.tcp != nil {
		return a.tcp.LocalAddr()
	}
	return a.udp.LocalAddr()
}

func newAssisConnByConn(raw net.Conn, server *AssisServer, timeout time.Duration) (conn *AssisConn) {
	conn = &AssisConn{
		Type:    "tcp",
		tcp:     raw,
		rwc:     frame.NewReadWriteCloser(raw, server.BufferSize),
		timeout: timeout,
		server:  server,
	}
	return
}

func newAssisConnByPacket(raw net.PacketConn, server *AssisServer, from net.Addr, timeout time.Duration) (conn *AssisConn) {
	conn = &AssisConn{
		Type:    "udp",
		udp:     raw,
		from:    from,
		timeout: timeout,
		server:  server,
	}
	return
}

//AssisVerifier is interface for verify by options
type AssisVerifier interface {
	Verify(conn *AssisConn, options xmap.M) (attrs xmap.M, err error)
}

//AssisVerifierF is func to implement AssisVerifier
type AssisVerifierF func(conn *AssisConn, options xmap.M) (attrs xmap.M, err error)

//Verify will verify the connection by options
func (a AssisVerifierF) Verify(conn *AssisConn, options xmap.M) (attrs xmap.M, err error) {
	attrs, err = a(conn, options)
	return
}

//AssisAccessAll will access all connection
type AssisAccessAll struct {
}

//NewAssisAccessAll will return new all access
func NewAssisAccessAll() *AssisAccessAll {
	return &AssisAccessAll{}
}

//Verify will access all conenction
func (a *AssisAccessAll) Verify(conn *AssisConn, options xmap.M) (attrs xmap.M, err error) {
	attrs = options
	return
}

//AssisServer is p2p assistant server
type AssisServer struct {
	BufferSize int
	Verifier   AssisVerifier
	delay      time.Duration
	locker     sync.RWMutex
	closer     chan int
	waiter     sync.WaitGroup
	conns      map[string]*AssisConn
	acces      map[string]io.Closer
}

//NewAssisServer will return new server by listener/verifier
func NewAssisServer(verifier AssisVerifier) (server *AssisServer) {
	server = &AssisServer{
		BufferSize: 2 * 1024,
		Verifier:   verifier,
		delay:      time.Second,
		conns:      map[string]*AssisConn{},
		locker:     sync.RWMutex{},
		waiter:     sync.WaitGroup{},
		acces:      map[string]io.Closer{},
	}
	return
}

//Start will start accept by uri set which is seperated by comma
func (a *AssisServer) Start(uris string) error {
	all := strings.Split(uris, ",")
	for _, uri := range all {
		u, err := url.Parse(uri)
		if err != nil {
			return err
		}
		var timeout time.Duration = 30
		err = attrvalid.Values(u.Query()).ValidFormat(`
			timeout,o|i,r:0;
		`, &timeout)
		if u.Scheme == "udp" {
			p, err := net.ListenPacket("udp", u.Host)
			if err != nil {
				return err
			}
			a.StartPacket(p, timeout*time.Second)
		} else {
			l, err := net.Listen(u.Scheme, u.Host)
			if err != nil {
				return err
			}
			a.StartListener(l, timeout*time.Second)
		}
	}
	return nil
}

//StartListener will start accept listener
func (a *AssisServer) StartListener(listener net.Listener, timeout time.Duration) {
	a.locker.Lock()
	defer a.locker.Unlock()
	a.acces[fmt.Sprintf("%v", listener)] = listener
	a.waiter.Add(1)
	go a.runAccept(listener, timeout)
	return
}

//StartPacket will start read by packet connection
func (a *AssisServer) StartPacket(packet net.PacketConn, timeout time.Duration) {
	a.locker.Lock()
	defer a.locker.Unlock()
	a.acces[fmt.Sprintf("%v", packet)] = packet
	a.waiter.Add(1)
	go a.runPacket(packet, timeout)
	return
}

//StartTimeout will start timeout runner
func (a *AssisServer) StartTimeout() {
	if a.closer != nil {
		return
	}
	a.waiter.Add(1)
	a.closer = make(chan int, 1)
	go a.runTimeout()
}

//Stop will stop the accept/timeout runner and close all connection
func (a *AssisServer) Stop() {
	a.locker.Lock()
	for key, acce := range a.acces {
		acce.Close()
		delete(a.acces, key)
	}
	for key, conn := range a.conns {
		conn.Close()
		delete(a.conns, key)
	}
	a.locker.Unlock()
	if a.closer != nil {
		a.closer <- 1
	}
	a.waiter.Wait()
	if a.closer != nil {
		close(a.closer)
	}
}

func (a *AssisServer) runAccept(listener net.Listener, timeout time.Duration) (err error) {
	log.Infof("AssisServer is starting on listener %v by timeout %v", listener.Addr(), timeout)
	var conn net.Conn
	for {
		conn, err = listener.Accept()
		if err != nil {
			break
		}
		go a.procConn(conn, timeout)
	}
	log.Infof("AssisServer runner on %v is stopped by %v", listener, err)
	a.waiter.Done()
	return
}

func (a *AssisServer) runPacket(conn net.PacketConn, timeout time.Duration) (err error) {
	log.Infof("AssisServer is starting receive packet %v by timeout %v", conn.LocalAddr(), timeout)
	for {
		buffer := make([]byte, a.BufferSize)
		if timeout > 0 {
			conn.SetReadDeadline(time.Now().Add(timeout))
		}
		n, from, rerr := conn.ReadFrom(buffer)
		if rerr != nil {
			err = rerr
			break
		}
		go a.procPacket(conn, from, buffer[0:n], timeout)
	}
	log.Infof("AssisServer packet runner on %v is stopped by %v", conn, err)
	a.waiter.Done()
	return
}

func (a *AssisServer) procConn(raw net.Conn, timeout time.Duration) (err error) {
	log.Debugf("AssisServer start process tcp connection from %v", raw.RemoteAddr())
	defer func() {
		if err != nil {
			raw.Close()
		}
		log.Debugf("AssisServer process tcp connection from %v is done by %v", raw.RemoteAddr(), err)
	}()
	conn := newAssisConnByConn(raw, a, timeout)
	if timeout > 0 {
		conn.rwc.SetTimeout(timeout)
	}
	buffer, err := conn.rwc.ReadFrame()
	if err == nil {
		err = a.procFrame(conn, buffer[frame.OffsetBytes:])
	}
	return
}

func (a *AssisServer) procPacket(raw net.PacketConn, from net.Addr, buffer []byte, timeout time.Duration) (err error) {
	conn := newAssisConnByPacket(raw, a, from, timeout)
	err = a.procFrame(conn, buffer[frame.OffsetBytes:])
	return
}

func (a *AssisServer) procFrame(conn *AssisConn, buffer []byte) (err error) {
	defer func() {
		if err != nil {
			log.Warnf("AssisServer process %v bytes frame from %v fail with %v", len(buffer), conn.RemoteAddr(), err)
		}
	}()
	options := xmap.M{}
	err = json.Unmarshal(buffer, &options)
	if err != nil {
		err = fmt.Errorf("parse login buffer to json fail with %v", err)
		xio.WriteJSON(conn, xmap.M{"code": 10, "err": err.Error()})
		return
	}
	err = options.ValidFormat(`
		uuid,r|s,l:0;
		waiting,r|s,l:0;
	`, &conn.UUID, &conn.Waiting)
	if err != nil {
		err = fmt.Errorf("verify login options with %v", err)
		xio.WriteJSON(conn, xmap.M{"code": 10, "err": err.Error()})
		return
	}
	conn.M, err = a.Verifier.Verify(conn, options)
	if err != nil {
		err = fmt.Errorf("verify login fail with %v", err)
		xio.WriteJSON(conn, xmap.M{"code": 10, "err": err.Error()})
		return
	}
	conn.M["public_addr"] = conn.RemoteAddr().String()
	conn.M["type"] = conn.Type
	conn.startTime = time.Now()
	log.Infof("AssisServer accepted on waiting connection from %v", conn.RemoteAddr())
	a.locker.Lock()
	old := a.conns[conn.UUID]
	waiting, ok := a.conns[conn.Waiting]
	if ok {
		delete(a.conns, conn.Waiting)
	} else {
		a.conns[conn.UUID] = conn
	}
	a.locker.Unlock()
	if old != nil {
		old.Close()
	}
	if ok {
		_, err = xio.WriteJSON(conn, xmap.M{
			"code":        0,
			"type":        conn.Type,
			"remote":      waiting.M,
			"public_addr": conn.RemoteAddr().String(),
		})
		if err == nil {
			_, err = xio.WriteJSON(waiting, xmap.M{
				"code":        0,
				"type":        waiting.Type,
				"remote":      conn.M,
				"public_addr": waiting.RemoteAddr().String(),
			})
		}
		conn.Close()
		waiting.Close()
		log.Infof("AssisServer waiting connection is done by %v to %v from %v", err, waiting.RemoteAddr(), conn.RemoteAddr())
		log.Infof("AssisServer waiting connection is done by %v to %v from %v", err, conn.RemoteAddr(), waiting.RemoteAddr())
	}
	return
}

func (a *AssisServer) runTimeout() {
	log.Infof("AssisServer start timeout connection runner")
	var running = true
	for running {
		select {
		case <-a.closer:
			running = false
		case <-time.After(a.delay):
			a.procTimeout()
		}
	}
	log.Infof("AssisServer connection timeout runner is stopped")
	a.waiter.Done()
}

func (a *AssisServer) procTimeout() {
	conns := []*AssisConn{}
	now := time.Now()
	a.locker.Lock()
	for key, conn := range a.conns {
		if now.Sub(conn.startTime) < conn.timeout {
			continue
		}
		conns = append(conns, conn)
		delete(a.conns, key)
	}
	a.locker.Unlock()
	if len(conns) > 0 {
		log.Infof("AssisServer will timeout %v connection", len(conns))
	}
	for _, conn := range conns {
		conn.Close()
	}
}

var Listener = net.ListenConfig{
	Control: Control,
}

func AssisPunching(uri string) (remote xmap.M, err error) {
	u, err := url.Parse(uri)
	if err != nil {
		return
	}
	query := u.Query()
	options := xmap.M{}
	for key := range query {
		options[key] = query.Get(key)
	}
	var (
		bufferSize = 1500
		timeout    = 32 * time.Second
	)
	err = options.ValidFormat(`
		buffer_size,o|i,r:0~10240;
		timeout,o|i,r:0;
	`, &bufferSize, &timeout)
	if err == nil {
		remote, err = AssisNetworkPunching(u.Scheme, u.Host, bufferSize, timeout, options)
	}
	return
}

func AssisNetworkPunching(network, address string, bufferSize int, timeout time.Duration, options xmap.M) (remote xmap.M, err error) {
	log.Infof("AssisClient start dial to assistant server %v://%v", network, address)
	defer func() {
		log.Infof("AssisClient network punching from assistant server %v://%v is done by %v", network, address, err)
	}()
	dialer := net.Dialer{Control: Control}
	raw, err := dialer.Dial(network, address)
	if err != nil {
		err = fmt.Errorf("dial to assistant server %v fail with %v", address, err)
		return
	}
	defer raw.Close()
	rwc := frame.NewReadWriteCloser(raw, bufferSize)
	if timeout > 0 {
		rwc.SetTimeout(timeout)
	}
	log.Debugf("AssisClient send to assistant server %v://%v by options:%v", network, address, converter.JSON(options))
	options["local_addr"] = raw.LocalAddr().String()
	_, err = xio.WriteJSON(rwc, options)
	if err != nil {
		err = fmt.Errorf("write to assistant server %v fail with %v", address, err)
		return
	}
	log.Debugf("AssisClient start wait remote from assistant server %v://%v", network, address)
	buffer, err := rwc.ReadFrame()
	if err != nil {
		err = fmt.Errorf("read from assistant server %v fail with %v", address, err)
		return
	}
	remote = xmap.M{}
	err = json.Unmarshal(buffer[frame.OffsetBytes:], &remote)
	if err != nil {
		err = fmt.Errorf("parse response json from assistant server %v fail with %v", address, err)
		return
	}
	if remote.IntDef(-1, "code") != 0 {
		err = fmt.Errorf("assistant server response code:%v,err:%v", remote.Int("code"), remote.Str("err"))
		return
	}
	remote["local_addr"] = raw.LocalAddr().String()
	return
}

func AssisEstablishUDP(loacalPrivateAddr, localPublicAddr, remotePrivateAddr, remotePublicAddr string, timeout time.Duration) (conn *net.UDPConn, remote *net.UDPAddr, err error) {
	localPrivate, err := net.ResolveUDPAddr("udp", loacalPrivateAddr)
	if err != nil {
		return
	}
	localPublic, err := net.ResolveUDPAddr("udp", localPublicAddr)
	if err != nil {
		return
	}
	remotePrivate, err := net.ResolveUDPAddr("udp", remotePrivateAddr)
	if err != nil {
		return
	}
	remotePublic, err := net.ResolveUDPAddr("udp", remotePublicAddr)
	if err != nil {
		return
	}
	packet, err := Listener.ListenPacket(context.Background(), "udp", loacalPrivateAddr)
	if err != nil {
		return
	}
	sendPrivate := !bytes.Equal(localPrivate.IP, localPublic.IP) && bytes.Equal(localPublic.IP, remotePublic.IP) &&
		bytes.Equal(localPrivate.IP, remotePrivate.IP)
	conn = packet.(*net.UDPConn)
	message := uuid.New()
	notsend, notrecv := true, true
	go func() {
		for i := 0; i < 10 && notsend; i++ {
			if sendPrivate {
				_, err = conn.WriteTo([]byte(message), remotePrivate)
				if err != nil {
					break
				}
			}
			_, err = conn.WriteTo([]byte(message), remotePublic)
			if err != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	var n int
	var from net.Addr
	buffer := make([]byte, 3*1024)
	for notsend || notrecv {
		conn.SetReadDeadline(time.Now().Add(timeout))
		n, from, err = conn.ReadFrom(buffer)
		if err != nil {
			break
		}
		if string(buffer[0:n]) == message {
			remote = from.(*net.UDPAddr)
			notsend = false
		} else {
			conn.WriteTo(buffer[0:n], from)
			notrecv = false
		}
	}
	return
}

func AssisEstablishTCP(loacalPrivateAddr, localPublicAddr, remotePrivateAddr, remotePublicAddr string, listen bool, timeout time.Duration) (conn *net.TCPConn, remote *net.TCPAddr, err error) {
	localPrivate, err := net.ResolveTCPAddr("tcp", loacalPrivateAddr)
	if err != nil {
		return
	}
	localPublic, err := net.ResolveTCPAddr("tcp", localPublicAddr)
	if err != nil {
		return
	}
	remotePrivate, err := net.ResolveTCPAddr("tcp", remotePrivateAddr)
	if err != nil {
		return
	}
	remotePublic, err := net.ResolveTCPAddr("tcp", remotePublicAddr)
	if err != nil {
		return
	}
	waiter := make(chan int)
	locker := sync.RWMutex{}
	connected := func(raw net.Conn, r *net.TCPAddr, done bool) {
		locker.Lock()
		defer locker.Unlock()
		if !done {
			if conn != nil || raw == nil {
				return
			}
			conn, remote = raw.(*net.TCPConn), r
		}
		if waiter != nil {
			close(waiter)
			waiter = nil
		}
	}
	trydial := func(dialer *net.Dialer, addr *net.TCPAddr) {
		for i := 0; i < 10 && conn == nil; i++ {
			raw, xerr := dialer.Dial("tcp", addr.String())
			log.Debugf("try dial to %v done by %v", addr, xerr)
			if xerr == nil {
				connected(raw, addr, false)
				break
			}
		}
		connected(nil, nil, true)
	}
	var listener *net.TCPListener
	if listen {
		var l net.Listener
		l, err = Listener.Listen(context.Background(), "tcp", loacalPrivateAddr)
		if err != nil {
			return
		}
		listener = l.(*net.TCPListener)
		go func() {
			log.Debugf("start listen on local private address %v", loacalPrivateAddr)
			raw, xerr := listener.AcceptTCP()
			if xerr == nil {
				connected(raw, raw.RemoteAddr().(*net.TCPAddr), false)
			} else {
				connected(nil, nil, true)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	if !bytes.Equal(localPrivate.IP, localPublic.IP) &&
		bytes.Equal(localPublic.IP, remotePublic.IP) &&
		bytes.Equal(localPrivate.IP, remotePrivate.IP) {
		go func() {
			log.Debugf("start dial to remote private address %v", remotePrivateAddr)
			dialer := &net.Dialer{Timeout: timeout}
			trydial(dialer, remotePrivate)
		}()
	}
	{
		go func() {
			log.Debugf("start dial to remote public address %v", remotePublicAddr)
			dialer := &net.Dialer{Control: Control, LocalAddr: localPrivate, Timeout: timeout}
			trydial(dialer, remotePublic)
			if listener != nil {
				listener.Close()
			}
		}()
	}
	<-waiter
	if conn == nil {
		err = fmt.Errorf("connect fail")
	}
	return
}

func Dial(uri string) (conn net.Conn, remoteAddr net.Addr, err error) {
	u, err := url.Parse(uri)
	if err != nil {
		return
	}
	query := u.Query()
	options := xmap.M{}
	for key := range query {
		options[key] = query.Get(key)
	}
	var (
		local       string
		remote      string
		listen      int
		waitTimeout time.Duration
		connTimeout time.Duration
	)
	err = options.ValidFormat(`
		local,r|s,l:0;
		remote,r|s,l:0;
		listen,o|i,o:0~1;
		wait_timeout,o|i,r:0;
		conn_timeout,o|i,r:0;
	`, &local, &remote, &listen, &waitTimeout, &connTimeout)
	if err == nil {
		conn, remoteAddr, err = DialNetwork(u.Scheme, u.Host, listen == 1, local, remote, waitTimeout*time.Second, connTimeout*time.Second, options)
	}
	return
}

func DialNetwork(network, assistant string, listen bool, localID, remoteID string, punchingTimeout, dialTimeout time.Duration, options xmap.M) (conn net.Conn, remote net.Addr, err error) {
	options["uuid"] = localID
	options["waiting"] = remoteID
	info, err := AssisNetworkPunching(network, assistant, 3*1024, punchingTimeout, options)
	if err != nil {
		return
	}
	var localPrivateAddr, localPublicAddr, remotePrivateAddr, remotePublicAddr string
	err = info.ValidFormat(`
		local_addr,r|s,l:0;
		public_addr,r|s,l:0;
		remote/local_addr,r|s,l:0;
		remote/public_addr,r|s,l:0;
	`, &localPrivateAddr, &localPublicAddr, &remotePrivateAddr, &remotePublicAddr)
	if err != nil {
		return
	}
	time.Sleep(1000 * time.Millisecond)
	if network == "tcp" {
		conn, remote, err = AssisEstablishTCP(localPrivateAddr, localPublicAddr, remotePrivateAddr, remotePublicAddr, listen, dialTimeout)
	} else {
		conn, remote, err = AssisEstablishUDP(localPrivateAddr, localPublicAddr, remotePrivateAddr, remotePublicAddr, dialTimeout)
	}
	return
}
