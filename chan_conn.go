package hub

import "fmt"

// ChanConn is the connection given to the topic handlers
// it contains the connection information and the context of
// the current channel
type ChanConn struct {
	Conn *Conn

	Topic string
	Room  string
}

// SendJSON sends a message to the client connection
func (c ChanConn) SendJSON(event string, payload interface{}) error {
	return c.Conn.SendJSON(c.Topic, c.Room, event, payload)
}

// GetChannel gets the fully named channel in the form of "<topic>:<room>"
func (c ChanConn) GetChannel() string {
	return fmt.Sprintf("%s:%s", c.Topic, c.Room)
}
