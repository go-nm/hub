package hub

import (
	"encoding/json"
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

// ConnValidHandler is a callback function for the pre-connect
// validation step to allow users to specify a handler that allows or denys
// the wescoket connection based on the request.
type ConnValidHandler func(*http.Request) (interface{}, error)

// TopicHandler is the interface that must be implemented to register
// a new topic handler with the Hub for processing messages
type TopicHandler interface {
	Join(conn ChanConn, msg interface{}) error
	Receive(conn ChanConn, event string, msg interface{})
	Leave(conn ChanConn)
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

	connHandlers  map[string]ConnValidHandler
	topicHandlers map[string]TopicHandler

	clients map[*websocket.Conn]*Conn
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
		clients:       make(map[*websocket.Conn]*Conn),
		topicHandlers: make(map[string]TopicHandler),
		connHandlers:  make(map[string]ConnValidHandler),
	}

	go h.runPinger()

	return
}

// AddConnValidHandler registers a new connection validation handler
func (h *Hub) AddConnValidHandler(name string, handle ConnValidHandler) {
	h.connHandlers[name] = handle
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

// ServeHTTP is the HTTP handler for WebSocket connections to go to
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn := &Conn{
		Channels: make(map[string]bool),
		InitData: make(map[string]interface{}),
	}

	for key, validator := range h.connHandlers {
		data, err := validator(r)
		if err != nil {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "validationFailed",
				"error":  err,
			})
			return
		}
		conn.InitData[key] = data
	}

	// Upgrade the connection to a websocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer h.disconnect(conn)

	conn.WS = ws

	// Add to the list of clients
	h.clients[ws] = conn

	// Setup handlers and timeouts
	ws.SetReadLimit(h.opts.MaxMessageSize)
	ws.SetReadDeadline(time.Now().Add(*h.opts.PongWait))
	ws.SetPongHandler(func(string) error {
		ws.SetReadDeadline(time.Now().Add(*h.opts.PongWait))
		return nil
	})

	// Enter event loop
	for {
		// Read the message
		var msg coreMessage
		err := ws.ReadJSON(&msg)
		if err != nil {
			// Log errors if not expected message
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Println("[ERR] unexpected close error:", err)
				break
			}

			// Break if the error is an expected websocket error
			if e, ok := err.(*websocket.CloseError); ok && e.Code == websocket.CloseGoingAway {
				break
			}

			log.Println("[ERR] error reading json:", err)
			break
		}

		chanConn := ChanConn{Conn: conn, Topic: msg.Topic, Room: msg.Room}

		// Check that the topic for the message exists
		topic := h.topicHandlers[msg.Topic]
		if topic == nil {
			chanConn.SendJSON("not_found", statusPayload{Status: "error", Error: "topic not found"})
			continue
		}

		if msg.Event == "join" {
			// If the user is already joined dont rejoin
			if conn.Channels[chanConn.GetChannel()] == true {
				chanConn.SendJSON("already_joined", statusPayload{Status: "error", Error: "already joined topic"})
				continue
			}

			// If the topic handler returns an error dont join
			if err := topic.Join(chanConn, msg.Payload); err != nil {
				chanConn.SendJSON("join_failed", statusPayload{Status: "error", Error: err.Error()})
				continue
			}

			conn.Channels[chanConn.GetChannel()] = true

			chanConn.SendJSON("joined", statusPayload{Status: "success"})
		} else if msg.Event == "leave" {
			if conn.Channels[chanConn.GetChannel()] == true {
				topic.Leave(chanConn)
				conn.Channels[chanConn.GetChannel()] = false
			}

			// Respond with left message either way
			chanConn.SendJSON("left", statusPayload{Status: "success"})
		} else if conn.Channels[chanConn.GetChannel()] == true {
			topic.Receive(chanConn, msg.Event, msg.Payload)
		} else {
			chanConn.SendJSON("not_joined", statusPayload{Status: "error", Error: "topic not joined"})
		}
	}
}

func (h *Hub) disconnect(conn *Conn) {
	for channel := range conn.Channels {
		splitChannel := strings.Split(channel, ":")

		channelConn := ChanConn{Conn: conn, Topic: splitChannel[0], Room: splitChannel[1]}

		h.topicHandlers[splitChannel[0]].Leave(channelConn)
	}

	conn.WS.Close()

	delete(h.clients, conn.WS)
}
