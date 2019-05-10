package hub

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var (
	// Time allowed to read the next pong message from the peer.
	defaultPongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	defaultPingPeriod = (defaultPongWait * 9) / 10

	// Maximum message size allowed from peer.
	defaultMaxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type coreMessage struct {
	Topic   string      `json:"topic"`
	Room    string      `json:"room"`
	Event   string      `json:"event"`
	Payload interface{} `json:"payload"`
}

type statusPayload struct {
	Status string `json:"description,omitempty"`
	Error  string `json:"error,omitempty"`
}

// TopicHandler is the interface that must be implemented to register
// a new topic handler with the Hub for processing messages
type TopicHandler interface {
	Join(conn Conn, msg interface{}) error
	Receive(conn Conn, event string, msg interface{})
	Unjoin(conn Conn)
}

// Opts is the struct for options passed into the hub
type Opts struct {
	PongWait       *time.Duration
	PingPeriod     *time.Duration
	MaxMessageSize int64
}

// Hub is the struct for the main Hub
type Hub struct {
	opts Opts

	topicHandlers map[string]TopicHandler

	clients map[*websocket.Conn][]string
}

// New creates a new hub and starts the ping service to keep connections alive
func New(opts *Opts) (h *Hub) {
	if opts == nil {
		opts = &Opts{}
	}
	if opts.PongWait == nil {
		opts.PongWait = &defaultPongWait
	}
	if opts.PingPeriod == nil {
		opts.PingPeriod = &defaultPingPeriod
	}
	if opts.MaxMessageSize == 0 {
		opts.MaxMessageSize = int64(defaultMaxMessageSize)
	}
	h = &Hub{
		opts:          *opts,
		clients:       make(map[*websocket.Conn][]string),
		topicHandlers: make(map[string]TopicHandler),
	}

	go h.runPinger()

	return
}

// AddTopicHandler registers a new TopicHandler with the topic name
func (h *Hub) AddTopicHandler(topic string, handler TopicHandler) {
	h.topicHandlers[topic] = handler
}

func (h *Hub) runPinger() {
	for {
		for conn := range h.clients {
			conn.WriteMessage(websocket.PingMessage, []byte{})
		}
		time.Sleep(*h.opts.PingPeriod)
	}
}

// Handler is the HTTP handler for WebSocket connections to go to
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
	conn.SetReadLimit(h.opts.MaxMessageSize)
	conn.SetReadDeadline(time.Now().Add(*h.opts.PongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(*h.opts.PongWait))
		return nil
	})

	// Enter event loop
	for {
		// Read the message
		var msg coreMessage
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
			channelConn.SendJSON("not_found", statusPayload{Status: "error", Error: "topic not found"})
			continue
		}

		if msg.Event == "join" {
			// If the user is already joined dont rejoin
			if h.isClientInTopic(channelConn, channelConn.GetChannel()) {
				channelConn.SendJSON("already_joined", statusPayload{Status: "error", Error: "already joined topic"})
				continue
			}

			if err := topic.Join(channelConn, msg.Payload); err != nil {
				channelConn.SendJSON("join_failed", statusPayload{Status: "error", Error: err.Error()})
				continue
			}

			h.clients[conn] = append(h.clients[conn], channelConn.GetChannel())

			channelConn.SendJSON("joined", statusPayload{Status: "success"})
		} else if h.isClientInTopic(channelConn, channelConn.GetChannel()) {
			topic.Receive(channelConn, msg.Event, msg.Payload)
		} else {
			channelConn.SendJSON("not_joined", statusPayload{Status: "error", Error: "topic not joined"})
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
