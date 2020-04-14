package vsock

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func contextID() (uint32, error) {
	f, err := os.Open(devVsock)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	return unix.IoctlGetUint32(int(f.Fd()), unix.IOCTL_VM_SOCKETS_GET_LOCAL_CID)
}

type listenFD interface {
	io.Closer
	EarlyClose() error
	Accept4(flags int) (connFD, unix.Sockaddr, error)
	Bind(socketAddress unix.Sockaddr) error
	Listen(n int) error
	Getsockname() (unix.Sockaddr, error)
	SetNonblocking(name string) error
	SetDeadline(t time.Time) error
}

var _ listenFD = &sysListenFD{}

type sysListenFD struct {
	fd int      // Used in blocking mode.
	f  *os.File // Used in non-blocking mode.
}

func newListenFD() (*sysListenFD, error) {
	fd, err := socket()
	if err != nil {
		return nil, err
	}
	return &sysListenFD{
		fd: fd,
	}, nil
}

func (self *sysListenFD) Bind(socketAddress unix.Sockaddr) error {
	return unix.Bind(self.fd, socketAddress)
}
func (self *sysListenFD) Getsockname() (unix.Sockaddr, error) { return unix.Getsockname(self.fd) }
func (self *sysListenFD) Listen(n int) error                  { return unix.Listen(self.fd, n) }

func (self *sysListenFD) SetNonblocking(name string) error {
	return self.setNonblocking(name)
}

func (self *sysListenFD) EarlyClose() error { return unix.Close(self.fd) }
func (self *sysListenFD) Accept4(flags int) (connFD, unix.Sockaddr, error) {
	newFD, socketAddress, err := self.accept4(flags)
	if err != nil {
		return nil, nil, err
	}

	cfd := &sysConnFD{fd: newFD}
	return cfd, socketAddress, nil
}

func (self *sysListenFD) Close() error                  { return self.f.Close() }
func (self *sysListenFD) SetDeadline(t time.Time) error { return self.setDeadline(t) }

// A connectionFD is a type that wraps a file descriptor used to implement net.Conn.
type connFD interface {
	io.ReadWriteCloser
	EarlyClose() error
	Connect(socketAddress unix.Sockaddr) error
	Getsockname() (unix.Sockaddr, error)
	Shutdown(how int) error
	SetNonblocking(name string) error
	SetDeadline(t time.Time, typ deadlineType) error
	SyscallConn() (syscall.RawConn, error)
}

var _ connFD = &sysConnFD{}

func newConnFD() (*sysConnFD, error) {
	if fd, err := socket(); err != nil {
		return nil, err
	} else {
		return &sysConnFD{
			fd: fd,
		}, nil
	}
}

// TODO: Using a file foor non-blocking, why not just use a fucking mutex?
type sysConnFD struct {
	fd int
	f  *os.File
}

func (self *sysConnFD) Connect(socketAddress unix.Sockaddr) error {
	return unix.Connect(self.fd, socketAddress)
}
func (self *sysConnFD) Getsockname() (unix.Sockaddr, error) { return unix.Getsockname(self.fd) }
func (self *sysConnFD) EarlyClose() error                   { return unix.Close(self.fd) }
func (self *sysConnFD) SetNonblocking(name string) error    { return self.setNonblocking(name) }
func (self *sysConnFD) Close() error                        { return self.f.Close() }
func (self *sysConnFD) Read(b []byte) (int, error)          { return self.f.Read(b) }
func (self *sysConnFD) Write(b []byte) (int, error)         { return self.f.Write(b) }

func (self *sysConnFD) Shutdown(how int) error {
	switch how {
	case unix.SHUT_RD, unix.SHUT_WR:
		return self.shutdown(how)
	default:
		return fmt.Errorf("vsock: sysConnFD.Shutdown method invoked with invalid how constant: %d", how)
	}
}

func (self *sysConnFD) SetDeadline(t time.Time, typ deadlineType) error {
	return self.setDeadline(t, typ)
}

func (self *sysConnFD) SyscallConn() (syscall.RawConn, error) { return self.syscallConn() }

func socket() (int, error) {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	switch err {
	case nil:
		return fd, nil
	case unix.EINVAL:
		syscall.ForkLock.RLock()
		defer syscall.ForkLock.RUnlock()

		fd, err = unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM, 0)
		if err != nil {
			return 0, err
		}
		unix.CloseOnExec(fd)

		return fd, nil
	default:
		return 0, err
	}
}

func isErrno(err error, errno int) bool {
	switch errno {
	case ebadf:
		return err == unix.EBADF
	case enotconn:
		return err == unix.ENOTCONN
	default:
		fmt.Errorf("vsock: isErrno called with unhandled error number parameter: %d", errno)
		return false
	}
}

func panicf(format string, a ...interface{}) {
	panic(fmt.Sprintf(format, a...))
}

func (self *sysListenFD) accept4(flags int) (newFD int, socketAddress unix.Sockaddr, err error) {
	// In Go 1.12+, we make use of runtime network poller integration to allow
	// net.Listener.Accept to be unblocked by a call to net.Listener.Close.
	rawConn, err := self.f.SyscallConn()
	if err != nil {
		return 0, nil, err
	}

	rawConn.Read(func(fd uintptr) bool {
		newFD, socketAddress, err = unix.Accept4(int(fd), flags)
		switch err {
		case unix.EAGAIN, unix.ECONNABORTED:
			return false
		default:
			return true
		}
	})

	return newFD, socketAddress, nil
}

func (self *sysListenFD) setDeadline(t time.Time) error { return self.f.SetDeadline(t) }

func (self *sysListenFD) setNonblocking(name string) error {
	if err := unix.SetNonblock(self.fd, true); err != nil {
		return err
	}

	self.f = os.NewFile(uintptr(self.fd), name)

	return nil
}

func (self *sysConnFD) shutdown(how int) error {
	rc, err := self.f.SyscallConn()
	if err != nil {
		return err
	}

	doErr := rc.Control(func(fd uintptr) {
		err = unix.Shutdown(int(fd), how)
	})
	if doErr != nil {
		return doErr
	}

	return err
}

func (self *sysConnFD) syscallConn() (syscall.RawConn, error) { return self.f.SyscallConn() }

func (self *sysConnFD) setNonblocking(name string) error {
	if err := unix.SetNonblock(self.fd, true); err != nil {
		return err
	}

	self.f = os.NewFile(uintptr(self.fd), name)

	return nil
}

func (self *sysConnFD) setDeadline(t time.Time, typ deadlineType) error {
	switch typ {
	case deadline:
		return self.f.SetDeadline(t)
	case readDeadline:
		return self.f.SetReadDeadline(t)
	case writeDeadline:
		return self.f.SetWriteDeadline(t)
	}
	return fmt.Errorf("vsock: sysConnFD.SetDeadline method invoked with invalid deadline type constant: %d", typ)
}
