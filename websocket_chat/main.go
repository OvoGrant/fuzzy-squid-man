package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"sync"
	"time"
)

const MaxChatRoomSize = 2
const NumberOfChatRooms = 1000
const EMPTY = "empty"
const LeaveMessage = "LEAVE"
const MatchMessage = "MATCH"
const ChatMessage = "CHAT"
const ConnectMessage = "CONNECT"

type Message struct {
	Sender      uuid.UUID `json:"sender_id"`
	Timestamp   time.Time `json:"timestamp"`
	Body        string    `json:"body"`
	MessageType string    `json:"message_type"'`
}

type Client struct {
	Id       uuid.UUID
	Language string
	Conn     *websocket.Conn
	Send     chan Message
}

type ChatRoom struct {
	Members  map[uuid.UUID]*Client
	Language string
	Lock     sync.Mutex
}

var ChatRooms = make([]*ChatRoom, NumberOfChatRooms)

func FindChatRoom(client *Client) (*ChatRoom, error) {

	for _, chatRoom := range ChatRooms {

		chatRoom.Lock.Lock()
		if len(chatRoom.Members) < MaxChatRoomSize &&
			(chatRoom.Language == client.Language || chatRoom.Language == EMPTY) {
			chatRoom.Members[client.Id] = client
			if chatRoom.Language == EMPTY {
				chatRoom.Language = client.Language
			}
			chatRoom.Lock.Unlock()
			return chatRoom, nil
		}

		chatRoom.Lock.Unlock()
	}
	log.Println("Could not find a chat room")
	return nil, errors.New("could not find a chat room")

}

func (cr *ChatRoom) broadcastMessage(message Message) {
	for _, member := range cr.Members {
		if member.Id != message.Sender {
			member.Send <- message
		}
	}
}

func readMessages(client *Client) {
	for message := range client.Send {
		err := client.Conn.WriteJSON(message)

		if err != nil {
			log.Println("Error writing to socket", err)
		}
	}
}

func (cr *ChatRoom) leave(client *Client) {
	cr.Lock.Lock()
	delete(cr.Members, client.Id)
	if len(cr.Members) == 0 {
		cr.Language = EMPTY
	}
	cr.Lock.Unlock()
}

func (cr *ChatRoom) broadcast(message Message) {
	cr.Lock.Lock()
	for _, v := range cr.Members {
		v.Send <- message
	}
	cr.Lock.Unlock()
}

var connectionUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func handleConnection(w http.ResponseWriter, r *http.Request) {

	conn, err := connectionUpgrader.Upgrade(w, r, nil)

	if err != nil {
		log.Println("Failed to upgrade to WebSocket")
		http.Error(w, "Failed to upgrade to WebSocket", http.StatusInternalServerError)
		return
	}

	_, msg, err := conn.ReadMessage()

	if err != nil {
		log.Println("Could not read message from client", err)
		return
	}

	var message Message
	err = json.Unmarshal(msg, &message)

	if err != nil {
		log.Println("Could not parse message from client", err)
		return
	}

	client := Client{message.Sender, message.Body, conn, make(chan Message)}

	chatRoom, err := FindChatRoom(&client)

	if err != nil {
		log.Println("Failed to find a chatroom for the client")
		http.Error(w, "Failed to find a chat room try again", http.StatusInternalServerError)
		return
	}

	defer func() {
		conn.Close()
		chatRoom.leave(&client)
		chatRoom.broadcast(Message{client.Id, time.Now(), "client left", LeaveMessage})
		close(client.Send)
	}()

	go readMessages(&client)
	log.Println("received request for " + string(msg))

	chatRoom.Lock.Lock()
	if len(chatRoom.Members) > 1 {
		chatRoom.broadcast(Message{client.Id, time.Now(), "You've been matched", MatchMessage})
	}
	chatRoom.Lock.Unlock()

	for {

		_, msg, err := conn.ReadMessage()

		fmt.Println(string(msg))

		if err != nil {
			log.Println("Error reading message. Client disconnected")
			break
		}

		var message Message
		err = json.Unmarshal(msg, &message)

		if err != nil {
			fmt.Println(err)
		}

		fmt.Println(message)

		chatRoom.broadcast(message)

	}

}

func main() {

	for i, _ := range ChatRooms {
		ChatRooms[i] = &ChatRoom{Members: make(map[uuid.UUID]*Client, MaxChatRoomSize), Language: EMPTY}
	}

	http.HandleFunc("/", handleConnection)
	http.ListenAndServe(":8080", nil)

}
