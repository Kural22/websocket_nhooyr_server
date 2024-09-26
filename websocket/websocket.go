package websocket

import (
	"context"
	"log"
	"net/http"
	"smaplechat/mongodb"
	"sync"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

var clients = make(map[*websocket.Conn]bool)
var broadcast = make(chan mongodb.ChatMessage, 200)
var mutex sync.Mutex

// var broadcastBatch []mongodb.ChatMessage
var batchMutex sync.Mutex

// HandleWebSocket manages new WebSocket connections

// for every connection made, handleWebsocket is called, means http is converted to websocket connection
// the new connection is added to the clients map and then all the previous messages are retrieved and sent back
// open for loop is maintained to read the new message, and then added to mongodb and pushed into broadcast chan.

// Golang has inbuilt channel, now handlemessages runs in background(as a goroutine), the last message is taken out
// this message is sent to all the connected clients.
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {

	startTime := time.Now()
	// log.Println("The read request will have ", r)
	// log.Println("The write request will have ", w)
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Println("WebSocket accept error:", err)
		return
	}

	defer func() {
		conn.Close(websocket.StatusInternalError, "Internal Error")
		disconnectTime := time.Now()
		elapsedTime := disconnectTime.Sub(startTime)
		log.Printf("Connection closed. Time connected: %v\n", elapsedTime)
	}()

	userID := r.URL.Query().Get("userID")
	if userID == "" {
		userID = uuid.New().String()
	}

	log.Printf("Connection established with user-id: %s. Time taken to connect: %v\n", userID, time.Since(startTime))

	mutex.Lock()
	clients[conn] = true
	mutex.Unlock()

	// Send chat history to the new connection
	messages, _ := mongodb.RetrieveMessages()
	for _, msg := range messages {
		wsjson.Write(r.Context(), conn, msg)
	}
	log.Println("The total number of clients will be", len(clients))
	for {
		var msg mongodb.ChatMessage
		err := wsjson.Read(r.Context(), conn, &msg)

		// log.Printf("The new msg received from user will be %v to %v", conn, msg)

		// closeStatus := websocket.CloseStatus(err)
		// if closeStatus == websocket.StatusNormalClosure || closeStatus == websocket.StatusGoingAway {
		// 	log.Printf("WebSocket closed: status = %v, reason = %v\n", closeStatus, "Client closed the connection.")
		// 	break
		// }

		if err != nil {
			// log.Println("WebSocket read error:", err)
			mutex.Lock()
			delete(clients, conn)
			mutex.Unlock()
			break
		}

		// Add the current time as a timestamp and store the message
		msg.Username = userID
		msg.Timestamp = time.Now()
		mongodb.StoreMessage(msg.Username, msg.Message)
		// msg.ID = insertedID

		// Broadcast the message with ID and timestamp to all clients
		broadcast <- msg
	}

}

// HandleMessages broadcasts messages to all connected clients
// so whenever ws.Write is called it actually calls the ws.onmessage in the client side, this actually renders as html elements
// func HandleMessages() {
// 	for {
// 		msg := <-broadcast
// 		// log.Printf("The message to be broadcasted will be %v", msg)
// 		// Send the message to all connected clients
// 		mutex.Lock()
// 		log.Println("The total number of clients will be", len(clients))
// 		for conn := range clients {
// 			err := wsjson.Write(context.Background(), conn, msg)
// 			// log.Printf("The broadcasting msg will be %v", msg)
// 			if err != nil {
// 				// log.Println("WebSocket write error:", err)
// 				conn.Close(websocket.StatusInternalError, "Internal Error")
// 				delete(clients, conn)
// 			}
// 		}
// 		mutex.Unlock()
// 		time.Sleep(5 * time.Second)
// 	}
// }

func HandleMessages(batchSize int, thresholdTime time.Duration) {
	broadcastBatch := make([]mongodb.ChatMessage, 0, batchSize)
	ticker := time.NewTicker(thresholdTime)
	defer ticker.Stop()

	for {
		select {
		case msg := <-broadcast:
			batchMutex.Lock()
			broadcastBatch = append(broadcastBatch, msg)
			batchMutex.Unlock()

		case <-ticker.C:
			batchMutex.Lock()
			if len(broadcastBatch) > 0 {
				sendBatchSize := batchSize
				if len(broadcastBatch) < batchSize {
					sendBatchSize = len(broadcastBatch)
				}
				sendBatchMessages(broadcastBatch[:sendBatchSize])

				broadcastBatch = broadcastBatch[sendBatchSize:]
			}
			batchMutex.Unlock()
		}
	}
}

func sendBatchMessages(broadcastBatch []mongodb.ChatMessage) {

	log.Println("The total number of connected clients will be", len(clients))
	log.Printf("Broadcasting %d messages", len(broadcastBatch))

	// Send the batch to all clients
	for conn := range clients {
		err := wsjson.Write(context.Background(), conn, broadcastBatch)
		if err != nil {
			conn.Close(websocket.StatusInternalError, "Internal Error")
			mutex.Lock()
			delete(clients, conn)
			mutex.Unlock()
		}
	}

	// Clear the batch after broadcasting
	broadcastBatch = nil
}
