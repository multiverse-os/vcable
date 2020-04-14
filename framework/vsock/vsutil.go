package vsock

import (
	"net"
	"time"
)

func Accept(l net.Listener, timeout time.Duration) (net.Conn, error) {
	// This function accommodates both Go1.12+ and Go1.11 functionality to allow
	// net.Listener.Accept to be canceled by net.Listener.Close.
	//
	// If a timeout is set, set up a timer to close the listener and either:
	// - Go 1.12+: unblock the call to Accept
	// - Go 1.11 : eventually halt the loop due to closed file descriptor
	//
	// For Go 1.12+, we could use vsock.Listener.SetDeadline, but this approach
	// using a timer works for Go 1.11 as well.
	cancel := func() {}
	if timeout != 0 {
		timer := time.AfterFunc(timeout, func() { _ = l.Close() })
		cancel = func() { timer.Stop() }
	}

	for {
		c, err := l.Accept()
		if err != nil {
			if nerr, ok := err.(net.Error); ok && nerr.Temporary() {
				time.Sleep(250 * time.Millisecond)
				continue
			}

			return nil, err
		}

		// Got a connection, stop the timer.
		cancel()
		return c, nil
	}
}

// IsHypervisor detects if this machine is a hypervisor by determining if
// /dev/vsock is available, and then if its context ID matches the one assigned
// to hosts.
func IsHypervisor() bool {
	cid, err := ContextID()
	if err != nil {
		return false
	}

	return cid == Host
}
