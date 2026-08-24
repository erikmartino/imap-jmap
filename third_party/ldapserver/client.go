package ldapserver

import (
	"bufio"
	"net"
	"sync"
	"time"

	ldap "github.com/lor00x/goldap/message"
)

type client struct {
	Numero      int
	srv         *Server
	rwc         net.Conn
	br          *bufio.Reader
	bw          *bufio.Writer
	wg          sync.WaitGroup
	closing     chan bool
	requestList map[int]*Message
	mutex       sync.Mutex
	rawData     []byte
	writeErr    error
}

func (c *client) GetConn() net.Conn {
	return c.rwc
}

func (c *client) GetRaw() []byte {
	return c.rawData
}

func (c *client) SetConn(conn net.Conn) {
	c.rwc = conn
	c.br = bufio.NewReader(c.rwc)
	c.bw = bufio.NewWriter(c.rwc)
}

func (c *client) GetMessageByID(messageID int) (*Message, bool) {
	if requestToAbandon, ok := c.requestList[messageID]; ok {
		return requestToAbandon, true
	}
	return nil, false
}

func (c *client) Addr() net.Addr {
	return c.rwc.RemoteAddr()
}

func (c *client) ReadPacket() (*messagePacket, error) {
	mP, err := readMessagePacket(c.br)
	c.rawData = make([]byte, len(mP.bytes))
	copy(c.rawData, mP.bytes)
	return mP, err
}

func (c *client) serve() {
	defer func() {
		if r := recover(); r != nil {
			// A panic while parsing or handling a message must never kill the
			// whole server process (a real LDAP server fails just the one
			// connection). Recover here so c.close() below still runs.
			Logger.Printf("client %d: recovered from panic while serving: %v", c.Numero, r)
		}
	}()
	defer c.close()

	c.closing = make(chan bool)
	if onc := c.srv.OnNewConnection; onc != nil {
		if err := onc(c.rwc); err != nil {
			Logger.Printf("Erreur OnNewConnection: %s", err)
			return
		}
	}

	// Listen for server signal to shutdown
	go func() {
		select {
		case <-c.srv.chDone: // server signals shutdown process
			r := NewExtendedResponse(LDAPResultUnwillingToPerform)
			r.SetDiagnosticMessage("server is about to stop")
			r.SetResponseName(NoticeOfDisconnection)

			m := ldap.NewLDAPMessageWithProtocolOp(r)
			c.writeMessage(m)
			c.rwc.SetReadDeadline(time.Now().Add(time.Millisecond))
		case <-c.closing:
		}
	}()

	c.requestList = make(map[int]*Message)

	for {

		if c.srv.ReadTimeout != 0 {
			c.rwc.SetReadDeadline(time.Now().Add(c.srv.ReadTimeout))
		}
		// NOTE: the write deadline is applied per-write inside writeMessage (a
		// deadline set here would expire on an idle connection and make the next
		// response write fail).

		//Read client input as a ASN1/BER binary message
		messagePacket, err := c.ReadPacket()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				Logger.Printf("Sorry client %d, i can not wait anymore (reading timeout) ! %s", c.Numero, err)
			} else {
				Logger.Printf("Error readMessagePacket: %s", err)
			}
			return
		}

		//Convert ASN1 binaryMessage to a ldap Message
		message, err := messagePacket.readMessage()

		if err != nil {
			Logger.Printf("Error reading Message : %s\n\t%x", err.Error(), messagePacket.bytes)
			continue
		}
		Logger.Printf("<<< %d - %s - hex=%x", c.Numero, message.ProtocolOpName(), messagePacket)

		// When message is an UnbindRequest, stop serving
		if _, ok := message.ProtocolOp().(ldap.UnbindRequest); ok {
			return
		}

		// Requests are processed synchronously, one at a time, and each response
		// is written directly to the connection. This avoids the unbounded
		// goroutine-per-request design (and its bounded, easily-overflowed
		// response queue) that stalled clients under pipelined load.
		c.ProcessRequestMessage(&message)
		if c.writeErr != nil {
			// A failed write (e.g. the peer stopped reading and the socket send
			// buffer filled up) leaves the connection in an unusable state. Close
			// it so the client reconnects instead of hanging on a zombie socket —
			// exactly what a real LDAP server does.
			Logger.Printf("client %d: closing after write error: %v", c.Numero, c.writeErr)
			return
		}
	}

}

// close closes client:
// * stop reading from client
// * signal to all running request processors to stop
// * close client connection
// * signal to server that client shutdown is ok
func (c *client) close() {
	Logger.Printf("client %d close()", c.Numero)
	close(c.closing)

	// stop reading from client
	c.rwc.SetReadDeadline(time.Now().Add(time.Millisecond))
	Logger.Printf("client %d close() - stop reading from client", c.Numero)

	// signals to all currently running request processor to stop
	c.mutex.Lock()
	for messageID, request := range c.requestList {
		Logger.Printf("Client %d close() - sent abandon signal to request[messageID = %d]", c.Numero, messageID)
		go request.Abandon()
	}
	c.mutex.Unlock()
	Logger.Printf("client %d close() - Abandon signal sent to processors", c.Numero)

	c.rwc.Close() // close client connection
	Logger.Printf("client [%d] connection closed", c.Numero)

	c.srv.wg.Done() // signal to server that client shutdown is ok
}

func (c *client) writeMessage(m *ldap.LDAPMessage) {
	data, _ := m.Write()
	c.mutex.Lock()
	defer c.mutex.Unlock()
	Logger.Printf(">>> %d - %s - hex=%x", c.Numero, m.ProtocolOpName(), data.Bytes())
	if c.writeErr != nil {
		return
	}
	if c.srv.WriteTimeout != 0 {
		c.rwc.SetWriteDeadline(time.Now().Add(c.srv.WriteTimeout))
	}
	if _, err := c.bw.Write(data.Bytes()); err != nil {
		c.writeErr = err
		return
	}
	if err := c.bw.Flush(); err != nil {
		c.writeErr = err
	}
}

// ResponseWriter interface is used by an LDAP handler to
// construct an LDAP response.
type ResponseWriter interface {
	// Write writes the LDAPResponse to the connection as part of an LDAP reply.
	Write(po ldap.ProtocolOp)
}

type responseWriterImpl struct {
	c         *client
	messageID int
}

func (w responseWriterImpl) Write(po ldap.ProtocolOp) {
	m := ldap.NewLDAPMessageWithProtocolOp(po)
	m.SetMessageID(w.messageID)
	w.c.writeMessage(m)
}

func (c *client) ProcessRequestMessage(message *ldap.LDAPMessage) {
	defer func() {
		if r := recover(); r != nil {
			Logger.Printf("client %d: recovered from panic while processing request: %v", c.Numero, r)
		}
	}()

	var m Message
	m = Message{
		LDAPMessage: message,
		Done:        make(chan bool, 2),
		Client:      c,
	}

	c.registerRequest(&m)
	defer c.unregisterRequest(&m)

	var w responseWriterImpl
	w.c = c
	w.messageID = m.MessageID().Int()

	c.srv.Handler.ServeLDAP(w, &m)
}

func (c *client) registerRequest(m *Message) {
	c.mutex.Lock()
	c.requestList[m.MessageID().Int()] = m
	c.mutex.Unlock()
}

func (c *client) unregisterRequest(m *Message) {
	c.mutex.Lock()
	delete(c.requestList, m.MessageID().Int())
	c.mutex.Unlock()
}
