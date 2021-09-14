package websocket

import (
	"bytes"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 24 * time.Hour//60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = 45 * time.Second//(pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var (
	newline = []byte{'\n'}
	space   = []byte{' '}
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type ClientType int

const (
	CC ClientType = iota
	Remote
)

type ConnectionStatus int

const (
	CandidateWithoutID ConnectionStatus = iota
	Connected
	AwaitingPassword
	AwaitingAuthentication
)

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	Send chan []byte

	Host string

	ClientType ClientType // Either "cc" or "remote"

	ConnectionStatus ConnectionStatus // both: CandidateWithoutID, Connected remote: AwaitingAuthentication

	ID int

	Challenge string

	Closed bool
}

func (c *Client) HandleIncomingMsg(data []byte) {
	if c.Closed {
		return
	}
	switch c.ConnectionStatus {
	case CandidateWithoutID:
		cid, err := strconv.Atoi(string(data))
		if err != nil {
			c.Unregister()
			return
		}
		c.ID = cid
		err = c.registerToClientPair()
		if err == nil {
			c.onRegister()
		}else {
			c.Unregister()
		}
	case Connected:
		c.communicate(data)
	case AwaitingPassword:
		c.handleAwaitingPassword(data)
	case AwaitingAuthentication:
		c.handleAuthentication(data)
	}
}

//communicate an internal function meant to handle communication between the 2 sides (cc and remote) only if the pair is established
func (c *Client) communicate(data []byte) {
	pair := c.hub.clientPairs[c.ID]
	if c.ClientType == Remote { // communicate should only be called when c is connected, so no need to check that.
		log.Println("R -> CC: " + string(data))
		pair.CC.Send <- data
		return
	}
	if c.ClientType == CC && pair.Remote != nil && pair.Remote.ConnectionStatus == Connected {
		log.Println("CC -> R: " + string(data))
		pair.Remote.Send <- data
		return
	}
	c.Unregister()
}

func (c *Client) registerToClientPair() error {
	err := c.hub.RegisterClientToClientPair(c)
	return err
}

func (c *Client) onRegister() {
	if c.ClientType == Remote {
		log.Println("A connection of client type 'Remote' is now registered to ID " + strconv.Itoa(c.ID) + " and switched to status 'AwaitingAuthentication'")
		c.ConnectionStatus = AwaitingAuthentication
		//builder := randstr()
		//c.Challenge = builder.String()
		//c.hub.clientPairs[c.ID].CC.Send <- []byte("print('" + c.Challenge + "')")
		return
	}
	if c.ClientType == CC {
		log.Println("A connection of client type 'CC' is now registered to ID " + strconv.Itoa(c.ID) + " and switched to status 'Connected'")
		c.ConnectionStatus = AwaitingPassword
		return
	}
}

func (c *Client) handleAuthentication(data []byte) {
	if c.hub.clientPairs[c.ID].CC.Challenge == string(data) {
		c.ConnectionStatus = Connected
		log.Println("A connection of client type 'Remote' with ID " + strconv.Itoa(c.ID) + " successfully authenticated!")
		return
	}
	log.Println("A connection of client type 'Remote' with ID " + strconv.Itoa(c.ID) + " failed to authenticate!")
	c.Unregister()
}

func (c *Client) handleAwaitingPassword(data []byte) {
	if len(string(data)) > 0 {
		log.Println("A connection of client type 'CC' with ID " + strconv.Itoa(c.ID) + " chose a password: " + string(data))
		c.Challenge = string(data)
		c.ConnectionStatus = Connected
		return
	}
	log.Println("A connection of client type 'CC' with ID " + strconv.Itoa(c.ID) + " failed to choose a password!")
	//c.Unregister()
}

func (c *Client) Unregister() {
	log.Println("Unregistered " + strconv.Itoa(c.ID))
	pair, ok := c.hub.clientPairs[c.ID]
	if ok {
		if pair.CC != nil {
			pair.CC.unregisterCurrentOnly()
		}
		if pair.Remote != nil {
			pair.Remote.unregisterCurrentOnly()
		}
		delete(c.hub.clientPairs, c.ID)
		return
	}
	c.unregisterCurrentOnly()
}

func (c *Client) unregisterCurrentOnly() {
	if c.Closed == true {
		return
	}
	c.Closed = true
	close(c.Send)
}

type ClientPair struct {
	ID     int
	CC     *Client
	Remote *Client
}

type IncomingMessage struct {
	client *Client

	data []byte
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		err := c.conn.Close()
		if err != nil {
			log.Println("Closed a closed socket")
		}
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	c.conn.SetCloseHandler(func(code int, text string) error {
		c.hub.unregister <- c
		err := c.conn.Close()
		if err != nil {
			log.Println("Closed a closed socket 2")
		}
		return nil
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("error: %v", err)
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))
		//c.hub.broadcast <- message
		c.hub.incoming <- &IncomingMessage{
			client: c,
			data:   message,
		}
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.Send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.Send)
			for i := 0; i < n; i++ {
				w.Write(newline)
				w.Write(<-c.Send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// serveWs handles websocket requests from the peer.
func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	client := &Client{ID: -1, hub: hub, conn: conn, Send: make(chan []byte, 256), Host: r.Host}
	client.hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

func randstr() strings.Builder {
	var output strings.Builder
	//Lowercase and Uppercase Both
	charSet := "abcdedfghijklmnopqrstABCDEFGHIJKLMNOP"
	length := 10 + rand.Intn(10)
	for i := 0; i < length; i++ {
		random := rand.Intn(len(charSet))
		randomChar := charSet[random]
		output.WriteString(string(randomChar))
	}
	return output
}
