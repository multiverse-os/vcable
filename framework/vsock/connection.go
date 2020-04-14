package vsock

import (
	"golang.org/x/sys/unix"
)

func newConn(cfd connFD, local, remote *Addr) (*Conn, error) {
	if err := cfd.SetNonblocking(local.fileName()); err != nil {
		return nil, err
	}

	return &Conn{
		fd:     cfd,
		local:  local,
		remote: remote,
	}, nil
}

func dial(cid, port uint32) (*Conn, error) {
	cfd, err := newConnFD()
	if err != nil {
		return nil, err
	}

	return dialLinux(cfd, cid, port)
}

func dialLinux(cfd connFD, cid, port uint32) (c *Conn, err error) {
	defer func() {
		if err != nil {
			_ = cfd.EarlyClose()
		}
	}()

	rsa := &unix.SockaddrVM{
		CID:  cid,
		Port: port,
	}

	if err := cfd.Connect(rsa); err != nil {
		return nil, err
	}

	lsa, err := cfd.Getsockname()
	if err != nil {
		return nil, err
	}

	lsavm := lsa.(*unix.SockaddrVM)

	local := &Addr{
		ContextID: lsavm.CID,
		Port:      lsavm.Port,
	}

	remote := &Addr{
		ContextID: cid,
		Port:      port,
	}

	return newConn(cfd, local, remote)
}
