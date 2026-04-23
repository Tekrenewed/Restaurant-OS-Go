package api

import (
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// CfdMessage acts as an envelope to route raw JSON payloads to the correct store's screens
type CfdMessage struct {
	StoreID uuid.UUID
	Payload []byte
}

// CfdClient represents a single Customer Facing Display (CFD) connected to the WebSocket
type CfdClient struct {
	Hub     *CfdHub
	Conn    *websocket.Conn
	StoreID uuid.UUID
	Send    chan []byte
}

// CfdHub maintains the set of active CFDs and broadcasts POS state updates to them
type CfdHub struct {
	// Registered clients mapped by StoreID
	Rooms      map[uuid.UUID]map[*CfdClient]bool
	Broadcast  chan CfdMessage
	Register   chan *CfdClient
	Unregister chan *CfdClient
}

// NewCfdHub initializes the central dispatch system for CFD screens
func NewCfdHub() *CfdHub {
	return &CfdHub{
		Rooms:      make(map[uuid.UUID]map[*CfdClient]bool),
		Broadcast:  make(chan CfdMessage),
		Register:   make(chan *CfdClient),
		Unregister: make(chan *CfdClient),
	}
}

// Run starts the infinite loop waiting for new connections or updates to broadcast
func (h *CfdHub) Run() {
	for {
		select {
		case client := <-h.Register:
			if h.Rooms[client.StoreID] == nil {
				h.Rooms[client.StoreID] = make(map[*CfdClient]bool)
			}
			h.Rooms[client.StoreID][client] = true
			log.Printf("CFD Screen connected to Store %s", client.StoreID)

		case client := <-h.Unregister:
			if _, ok := h.Rooms[client.StoreID][client]; ok {
				delete(h.Rooms[client.StoreID], client)
				close(client.Send)
				log.Printf("CFD Screen disconnected from Store %s", client.StoreID)
			}

		case msg := <-h.Broadcast:
			// Route to the specific store's CFD screens
			if clients, ok := h.Rooms[msg.StoreID]; ok {
				for client := range clients {
					select {
					case client.Send <- msg.Payload:
					default:
						close(client.Send)
						delete(h.Rooms[msg.StoreID], client)
					}
				}
			}
		}
	}
}

// ServeCfdWs handles websocket requests from the CFD React frontend
func (h *CfdHub) ServeCfdWs(w http.ResponseWriter, r *http.Request) {
	storeIDStr := r.URL.Query().Get("storeId")
	storeID, err := uuid.Parse(storeIDStr)
	if err != nil {
		http.Error(w, "Invalid storeId parameter missing", http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("CFD Upgrade Error:", err)
		return
	}

	client := &CfdClient{
		Hub:     h,
		Conn:    conn,
		StoreID: storeID,
		Send:    make(chan []byte, 256),
	}

	client.Hub.Register <- client

	// Allow Go to write the updates to this tablet
	go client.writePump()
}

func (c *CfdClient) writePump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()
	for {
		message, ok := <-c.Send
		if !ok {
			c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}

		w, err := c.Conn.NextWriter(websocket.TextMessage)
		if err != nil {
			return
		}
		w.Write(message)
		if err := w.Close(); err != nil {
			return
		}
	}
}
