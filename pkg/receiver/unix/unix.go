package receiver

import (
	"bufio"
	"fmt"
	"io"
	"net"

	"github.com/awesome-flow/flow/pkg/core"
	"github.com/awesome-flow/flow/pkg/metrics"
	evio_rcv "github.com/awesome-flow/flow/pkg/receiver/evio"
	"github.com/facebookgo/grace/gracenet"
	log "github.com/sirupsen/logrus"
)

const (
	MaxUnixPayloadSize = 65536
)

var (
	ErrMalformedUnixgram = fmt.Errorf("Malformed unixgram")
	ErrEmptyBody         = fmt.Errorf("Empty message body")
)

type Unix struct {
	Name     string
	listener net.Listener
	*core.Connector
}

func New(name string, params core.Params, context *core.Context) (core.Link, error) {
	path, ok := params["bind_addr"]
	if !ok {
		path = "/tmp/flow.sock"
	}

	if backend, ok := params["backend"]; ok {
		switch backend {
		case "evio":
			log.Info("Instantiating Evio backend for UNIX receiver")
			params["listeners"] = []interface{}{
				"unix://" + path.(string),
			}
			return evio_rcv.New(name, params, context)
		case "std":
		default:
			return nil, fmt.Errorf("Unknown backend: %s", backend)
		}
	}

	log.Info("Instantiating standard backend for UNIX receiver")

	net := &gracenet.Net{}
	lstnr, err := net.Listen("unix", path.(string))
	if err != nil {
		return nil, err
	}
	ux := &Unix{name, lstnr, core.NewConnector()}
	go func() {
		for {
			conn, err := lstnr.Accept()
			if err != nil {
				log.Errorf("Unix listener failed to call accept: %s", err.Error())
				continue
			}
			go ux.unixRecv(conn)
		}
	}()
	return ux, nil
}

func (ux *Unix) ExecCmd(cmd *core.Cmd) error {
	switch cmd.Code {
	case core.CmdCodeStop:
		if err := ux.listener.Close(); err != nil {
			log.Warnf("Failed to close unix socket properly: %s", err.Error())
		}
	}
	return nil
}

func (ux *Unix) unixRecv(conn net.Conn) {
	metrics.GetCounter("receiver.unix.conn.opened").Inc(1)
	reader := bufio.NewReader(conn)
	for {
		data, err := reader.ReadBytes('\n')
		metrics.GetCounter("receiver.unix.msg.received").Inc(1)

		if err != nil {
			if err == io.EOF {
				break
			}
			log.Warnf("Unix conn Read failed: %s", err)
			metrics.GetCounter("receiver.unix.msg.failed").Inc(1)
		}

		if len(data) == 0 {
			continue
		}
		msg := core.NewMessage(data)

		if sendErr := ux.Send(msg); sendErr != nil {
			log.Errorf("Unix socket failed to send message: %s", sendErr)
		} else {
			metrics.GetCounter("receiver.unix.msg.sent").Inc(1)
		}
	}
	if err := conn.Close(); err != nil {
		log.Errorf("Unix socket connection failed to close: %s", err)
	}
	metrics.GetCounter("receiver.unix.conn.closed").Inc(1)
}
