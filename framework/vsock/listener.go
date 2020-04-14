package vsock

import (
	"net"
	"time"

	"golang.org/x/sys/unix"
)

var _ net.Listener = &listener{}

type listener struct {
	fd   listenFD
	addr *Addr
}

func (self *listener) Addr() net.Addr                { return self.addr }
func (self *listener) Close() error                  { return self.fd.Close() }
func (self *listener) SetDeadline(t time.Time) error { return self.fd.SetDeadline(t) }

func (self *listener) Accept() (net.Conn, error) {
	// TODO(mdlayher): acquire syscall.ForkLock.RLock here once the Go 1.11
	// code can be removed and we're fully using the runtime network poller in
	// non-blocking mode.
	cfd, sa, err := self.fd.Accept4(unix.SOCK_CLOEXEC)
	if err != nil {
		return nil, err
	}

	savm := sa.(*unix.SockaddrVM)

	remote := &Addr{
		ContextID: savm.CID,
		Port:      savm.Port,
	}

	return newConn(cfd, self.addr, remote)
}

func listen(cid, port uint32) (*VsockListener, error) {
	lfd, err := newListenFD()
	if err != nil {
		return nil, err
	}

	return listenLinux(lfd, cid, port)
}

func listenLinux(lfd listenFD, cid, port uint32) (*VsockListener, error) {
	var err error
	defer func() {
		if err != nil {
			_ = lfd.EarlyClose()
		}
	}()

	if port == 0 {
		port = unix.VMADDR_PORT_ANY
	}

	sa := &unix.SockaddrVM{
		CID:  cid,
		Port: port,
	}

	if err := lfd.Bind(sa); err != nil {
		return nil, err
	}

	if err := lfd.Listen(unix.SOMAXCONN); err != nil {
		return nil, err
	}

	lsa, err := lfd.Getsockname()
	if err != nil {
		return nil, err
	}

	if err := lfd.SetNonblocking("vsock-listen"); err != nil {
		return nil, err
	}

	lsavm := lsa.(*unix.SockaddrVM)

	addr := &Addr{
		ContextID: lsavm.CID,
		Port:      lsavm.Port,
	}

	return &VsockListener{
		&listener{
			fd:   lfd,
			addr: addr,
		},
	}, nil
}
