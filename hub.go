package hub

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type CoreMessage struct {
	Topic   string      `json:"topic"`
	Room    string      `json:"room"`
	Event   string      `json:"event"`
	Payload interface{} `json:"payload"`
}

type StatusPayload struct {
	Status string `json:"description,omitempty"`
	Error  string `json:"error,omitempty"`
}

type TopicHandler interface {
	Join(conn Conn, msg interface{}) error
	Receive(conn Conn, event string, msg interface{})
	Unjoin(conn Conn)
}

type Hub struct {
	broadcast chan CoreMessage

	topicHandlers map[string]TopicHandler

	clients map[*websocket.Conn][]string
}

func New() (h *Hub) {
	h = &Hub{
		clients:       make(map[*websocket.Conn][]string),
		topicHandlers: make(map[string]TopicHandler),
	}

	go h.startBroadcaster()
	go h.runPinger()

	return
}

func (h *Hub) AddTopicHandler(topic string, handler TopicHandler) {
	h.topicHandlers[topic] = handler
}

func (h *Hub) runPinger() {
	for {
		for conn := range h.clients {
			conn.WriteMessage(websocket.PingMessage, []byte{})
		}
		time.Sleep(pingPeriod)
	}
}

// TODO determine if this is still needed at the hub level
func (h Hub) startBroadcaster() {
	for {
		msg := <-h.broadcast
		for conn := range h.clients {
			err := conn.WriteJSON(msg)
			if err != nil {
				log.Println("[ERR] could not write message:", err)
				h.disconnect(conn)
			}
		}
	}
}

func (h *Hub) Handler(w http.ResponseWriter, r *http.Request) {
	// Upgrade the connection to a websocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer h.disconnect(conn)

	// Add to the list of clients
	h.clients[conn] = []string{}

	// Setup handlers and timeouts
	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	// Enter event loop
	for {
		// Read the message
		var msg CoreMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			// Log errors if not expected message
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Println("[ERR] error reading json:", err)
				break
			}

			// Break if the error is an expected websocket error
			if e, ok := err.(*websocket.CloseError); ok && e.Code == websocket.CloseGoingAway {
				break
			}

			continue
		}

		channelConn := Conn{WSConn: conn, Topic: msg.Topic, Room: msg.Room}

		// Check that the topic for the message exists
		topic := h.topicHandlers[msg.Topic]
		if topic == nil {
			channelConn.SendJSON("not_found", StatusPayload{Status: "error", Error: "topic not found"})
			continue
		}

		if msg.Event == "join" {
			// If the user is already joined dont rejoin
			if h.isClientInTopic(channelConn, channelConn.GetChannel()) {
				channelConn.SendJSON("already_joined", StatusPayload{Status: "error", Error: "already joined topic"})
				continue
			}

			if err := topic.Join(channelConn, msg.Payload); err != nil {
				channelConn.SendJSON("join_failed", StatusPayload{Status: "error", Error: err.Error()})
				continue
			}

			h.clients[conn] = append(h.clients[conn], channelConn.GetChannel())

			channelConn.SendJSON("joined", StatusPayload{Status: "success"})
		} else if h.isClientInTopic(channelConn, channelConn.GetChannel()) {
			topic.Receive(channelConn, msg.Event, msg.Payload)
		} else {
			channelConn.SendJSON("not_joined", StatusPayload{Status: "error", Error: "topic not joined"})
		}
	}
}

func (h Hub) isClientInTopic(conn Conn, checkTopic string) bool {
	for _, topicName := range h.clients[conn.WSConn] {
		if checkTopic == topicName {
			return true
		}
	}

	return false
}

func (h *Hub) disconnect(conn *websocket.Conn) {
	log.Println("disconnecting client...")
	for _, channel := range h.clients[conn] {
		splitChannel := strings.Split(channel, ":")

		channelConn := Conn{WSConn: conn, Topic: splitChannel[0], Room: splitChannel[1]}

		h.topicHandlers[splitChannel[0]].Unjoin(channelConn)
	}

	conn.Close()

	delete(h.clients, conn)
}
