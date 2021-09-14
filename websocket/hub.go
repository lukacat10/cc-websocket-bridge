package websocket

import (
	"log"
	"strings"
)

// Hub maintains the set of active clients and broadcasts messages to the
// clients.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	clientPairs map[int]*ClientPair

	// Inbound messages from the clients.
	broadcast chan []byte

	// Inbound messages from the clients. Shall not be broadcasted
	incoming chan *IncomingMessage

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	handler MessageHandler
}

type RegisterClientToClientPairFailedError struct{}

func (m *RegisterClientToClientPairFailedError) Error() string {
	return "Failed to register a client to a client pair. Perhaps a previous connection wasn't terminated correctly?"
}

func (h *Hub) RegisterClientToClientPair(c *Client) error {
	if c.ClientType == CC {
		// ClientPair shouldn't exist. Ensure that...
		if _, ok := h.clientPairs[c.ID]; ok {
			log.Println("Found a user already registered with this ID! Closing...")
			c.unregisterCurrentOnly()
			return &RegisterClientToClientPairFailedError{}
		}
		h.clientPairs[c.ID] = &ClientPair{
			ID:     c.ID,
			CC:     c,
			Remote: nil,
		}
		return nil
	}
	if c.ClientType == Remote {
		// ClientPair should exist. Ensure that...
		if _, ok := h.clientPairs[c.ID]; !ok {
			log.Println("Found a user already registered with this ID! Closing...")
			c.unregisterCurrentOnly()
			return &RegisterClientToClientPairFailedError{}
		}
		h.clientPairs[c.ID].Remote = c
		return nil
	}
	return &RegisterClientToClientPairFailedError{}
}
func NewHub() *Hub {
	return &Hub{
		clientPairs: make(map[int]*ClientPair),
		incoming:  	make(chan *IncomingMessage),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		handler: 	nil,
	}
}

type MessageHandler func(*Client, []byte)

func (h *Hub) Handle(key string, handler MessageHandler) {
	h.handler = handler
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			addr := client.conn.RemoteAddr()
			if strings.HasPrefix(addr.String(), "127.0.0.1:") {
				client.ClientType = CC
				log.Println("A new connection of type 'CC' was made to the server!")
			} else {
				client.ClientType = Remote
				log.Println("A new connection of type 'Remote' was made to the server!")
			}
			log.Println("IP: " + addr.String())
			client.ConnectionStatus = CandidateWithoutID
			client.ID = -1
			client.Closed = false
		case client := <-h.unregister:
			client.Unregister()
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client)
				}
			}
		case message := <-h.incoming:
			//messageStr := string(message.data)
			message.client.HandleIncomingMsg(message.data)

		}
	}
}

