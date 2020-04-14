package vsock

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

const (
	// Hypervisor specifies that a socket should communicate with the hypervisor
	// process.
	Hypervisor = 0x0

	// Host specifies that a socket should communicate with processes other than
	// the hypervisor on the host machine.
	Host = 0x2

	// cidReserved is a reserved context ID that is no longer in use,
	// and cannot be used for socket communications.
	cidReserved = 0x1

	// shutRd and shutWr are arguments for unix.Shutdown, copied here to avoid
	// importing x/sys/unix in cross-platform code.
	shutRd = 0 // unix.SHUT_RD
	shutWr = 1 // unix.SHUT_WR

	// Error numbers we recognize, copied here to avoid importing x/sys/unix in
	// cross-platform code.
	ebadf    = 9
	enotconn = 107

	// devVsock is the location of /dev/vsock.  It is exposed on both the
	// hypervisor and on virtual machines.
	devVsock = "/dev/vsock"

	// network is the vsock network reported in net.OpError.
	network = "vsock"
)

type errOp int

const (
	opAccept errOp = iota
	opClose
	opDial
	opListen
	opRawControl
	opRawRead
	opRawWrite
	opRead
	opSet
	opSyscallConn
	opWrite
)

func (self errOp) String() string {
	switch self {
	case opAccept:
		return "accept"
	case opClose:
		return "close"
	case opDial:
		return "dial"
	case opListen:
		return "listen"
	case opRawControl:
		return "raw-control"
	case opRawRead:
		return "raw-read"
	case opRawWrite:
		return "raw-write"
	case opRead:
		return "read"
	case opSet:
		return "set"
	case opSyscallConn:
		return "syscall-conn"
	default: // opWrite:
		return "write"
	}
}

func Listen(port uint32) (*VsockListener, error) {
	cid, err := ContextID()
	if err != nil {
		// No addresses available.
		return nil, opError(opListen, err, nil, nil)
	}

	l, err := listen(cid, port)
	if err != nil {
		// No remote address available.
		return nil, opError(opListen, err, &Addr{
			ContextID: cid,
			Port:      port,
		}, nil)
	}

	return l, nil
}

var _ net.Listener = &VsockListener{}

type VsockListener struct {
	listener *listener
}

func (self *VsockListener) Accept() (net.Conn, error) {
	c, err := self.listener.Accept()
	if err != nil {
		return nil, self.opError(opAccept, err)
	}

	return c, nil
}

func (self *VsockListener) Addr() net.Addr {
	return self.listener.Addr()
}

func (self *VsockListener) Close() error { return self.opError(opClose, self.listener.Close()) }
func (self *VsockListener) SetDeadline(t time.Time) error {
	return self.opError(opSet, self.listener.SetDeadline(t))
}
func (self *VsockListener) opError(op errOp, err error) error {
	return opError(op, err, self.Addr(), nil)
}

func Dial(contextID, port uint32) (*Conn, error) {
	c, err := dial(contextID, port)
	if err != nil {
		return nil, opError(opDial, err, nil, &Addr{
			ContextID: contextID,
			Port:      port,
		})
	}

	return c, nil
}

var _ net.Conn = &Conn{}
var _ syscall.Conn = &Conn{}

type Conn struct {
	fd     connFD
	local  *Addr
	remote *Addr
}

func (self *Conn) Close() error         { return self.opError(opClose, self.fd.Close()) }
func (self *Conn) CloseRead() error     { return self.opError(opClose, self.fd.Shutdown(shutRd)) }
func (self *Conn) CloseWrite() error    { return self.opError(opClose, self.fd.Shutdown(shutWr)) }
func (self *Conn) LocalAddr() net.Addr  { return self.local }
func (self *Conn) RemoteAddr() net.Addr { return self.remote }

func (self *Conn) Read(b []byte) (int, error) {
	n, err := self.fd.Read(b)
	if err != nil {
		return n, self.opError(opRead, err)
	}

	return n, nil
}

func (self *Conn) Write(b []byte) (int, error) {
	n, err := self.fd.Write(b)
	if err != nil {
		return n, self.opError(opWrite, err)
	}

	return n, nil
}

// A deadlineType specifies the type of deadline to set for a Conn.
type deadlineType int

// Possible deadlineType values.
const (
	deadline deadlineType = iota
	readDeadline
	writeDeadline
)

func (self *Conn) SetDeadline(t time.Time) error {
	return self.opError(opSet, self.fd.SetDeadline(t, deadline))
}

func (self *Conn) SetReadDeadline(t time.Time) error {
	return self.opError(opSet, self.fd.SetDeadline(t, readDeadline))
}

func (self *Conn) SetWriteDeadline(t time.Time) error {
	return self.opError(opSet, self.fd.SetDeadline(t, writeDeadline))
}

// SyscallConn returns a raw network connection. This implements the
// syscall.Conn interface.
func (self *Conn) SyscallConn() (syscall.RawConn, error) {
	rc, err := self.fd.SyscallConn()
	if err != nil {
		return nil, self.opError(opSyscallConn, err)
	}
	return &rawConn{
		rc:     rc,
		local:  self.local,
		remote: self.remote,
	}, nil
}

func (c *Conn) opError(op errOp, err error) error { return opError(op, err, c.local, c.remote) }

var _ syscall.RawConn = &rawConn{}

type rawConn struct {
	rc     syscall.RawConn
	local  *Addr
	remote *Addr
}

func (self *rawConn) Control(fn func(fd uintptr)) error {
	return self.opError(opRawControl, self.rc.Control(fn))
}

func (self *rawConn) Read(fn func(fd uintptr) (done bool)) error {
	return self.opError(opRawRead, self.rc.Read(fn))
}

func (self *rawConn) Write(fn func(fd uintptr) (done bool)) error {
	return self.opError(opRawWrite, self.rc.Write(fn))
}

func (self *rawConn) opError(op errOp, err error) error {
	return opError(op, err, self.local, self.remote)
}

var _ net.Addr = &Addr{}

type Addr struct {
	ContextID uint32
	Port      uint32
}

func (self *Addr) Network() string { return network }

func (self *Addr) String() string {
	var host string
	switch self.ContextID {
	case Hypervisor:
		host = fmt.Sprintf("hypervisor(%d)", self.ContextID)
	case cidReserved:
		host = fmt.Sprintf("reserved(%d)", self.ContextID)
	case Host:
		host = fmt.Sprintf("host(%d)", self.ContextID)
	default:
		host = fmt.Sprintf("vm(%d)", self.ContextID)
	}
	return fmt.Sprintf("%s:%d", host, self.Port)
}

func (self *Addr) fileName() string { return fmt.Sprintf("%s:%s", self.Network(), self.String()) }
func ContextID() (uint32, error)    { return contextID() }

func opError(op errOp, err error, local, remote net.Addr) error {
	if err == nil {
		return nil
	}

	switch xerr := err.(type) {
	case *os.PathError:
		if xerr.Path != devVsock {
			err = xerr.Err
		}
	}

	switch {
	case err == io.EOF, isErrno(err, enotconn):
		return io.EOF
	case err == os.ErrClosed, isErrno(err, ebadf), strings.Contains(err.Error(), "use of closed"):
		err = errors.New("use of closed network connection")
	}

	var source, addr net.Addr
	switch op {
	case opClose, opDial, opRawRead, opRawWrite, opRead, opWrite:
		if local != nil {
			source = local
		}
		if remote != nil {
			addr = remote
		}
	case opAccept, opListen, opRawControl, opSet, opSyscallConn:
		if local != nil {
			addr = local
		}
	}

	return &net.OpError{
		Op:     op.String(),
		Net:    network,
		Source: source,
		Addr:   addr,
		Err:    err,
	}
}
