package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"telegraf/shared"
	"time"
)

type MessengerServer struct {
	storage     *DataStorage
	config      ServerConfig
	onlineUsers map[string]net.Conn
	mutex       sync.RWMutex
}

func NewMessengerServer(config ServerConfig, storageConfig Storage) *MessengerServer {
	storage := NewDataStorage(storageConfig)
	return &MessengerServer{
		storage:     storage,
		config:      config,
		onlineUsers: make(map[string]net.Conn),
	}
}

func (ms *MessengerServer) Start() error {
	if err := ms.storage.LoadAll(); err != nil {
		return fmt.Errorf("failed to load data: %v", err)
	}

	address := fmt.Sprintf("%s:%s", ms.config.Host, ms.config.Port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()

	log.Printf("Server started on %s", address)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go ms.handleConnection(conn)
	}
}

func (ms *MessengerServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	buffer := make([]byte, 4096)

	for {
		n, err := conn.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Printf("Read error: %v", err)
			}
			return
		}

		var request shared.Request
		if err := json.Unmarshal(buffer[:n], &request); err != nil {
			log.Printf("JSON unmarshal error: %v", err)
			continue
		}

		response := ms.handleRequest(request, conn)
		responseData, _ := json.Marshal(response)
		conn.Write(responseData)
	}
}

func (ms *MessengerServer) handleRequest(request shared.Request, conn net.Conn) shared.Response {
	switch request.Action {
	case "register":
		return ms.handleRegister(request)
	case "login":
		return ms.handleLogin(request, conn)
	case "recover":
		return ms.handleRecover(request)
	case "add_contact":
		return ms.handleAddContact(request)
	case "create_group":
		return ms.handleCreateGroup(request)
	case "send_message":
		return ms.handleSendMessage(request)
	case "get_messages":
		return ms.handleGetMessages(request)
	case "get_contacts":
		return ms.handleGetContacts(request)
	default:
		return shared.Response{Status: "error", Message: "Unknown action"}
	}
}

func (ms *MessengerServer) handleRegister(request shared.Request) shared.Response {
	if len(request.Username) > 16 || len(request.Password) > 16 {
		return shared.Response{Status: "error", Message: "Username and password must be max 16 characters"}
	}

	for _, char := range request.Username + request.Password {
		if char > 127 {
			return shared.Response{Status: "error", Message: "Only ASCII characters allowed"}
		}
	}

	if _, exists := ms.storage.GetUser(request.Username); exists {
		return shared.Response{Status: "error", Message: "Username already exists"}
	}

	passwordHash := fmt.Sprintf("%x", sha256.Sum256([]byte(request.Password)))
	user := &shared.User{
		Username: request.Username,
		Password: passwordHash,
		Email:    request.Email,
	}

	ms.storage.AddUser(user)

	if err := ms.storage.SaveUsers(); err != nil {
		return shared.Response{Status: "error", Message: err.Error()}
	}

	return shared.Response{Status: "success"}
}

func (ms *MessengerServer) handleLogin(request shared.Request, conn net.Conn) shared.Response {
	user, exists := ms.storage.GetUser(request.Username)
	if !exists {
		return shared.Response{Status: "error", Message: "Invalid credentials"}
	}

	passwordHash := fmt.Sprintf("%x", sha256.Sum256([]byte(request.Password)))
	if user.Password != passwordHash {
		return shared.Response{Status: "error", Message: "Invalid credentials"}
	}

	ms.mutex.Lock()
	ms.onlineUsers[request.Username] = conn
	ms.mutex.Unlock()

	return shared.Response{Status: "success"}
}

func (ms *MessengerServer) handleRecover(request shared.Request) shared.Response {
	user, exists := ms.storage.GetUser(request.Username)
	if !exists {
		return shared.Response{Status: "error", Message: "User not found"}
	}

	if user.Email != request.Email {
		return shared.Response{Status: "error", Message: "Email doesn't match"}
	}

	return shared.Response{
		Status:  "success",
		Message: user.Password,
	}
}

func (ms *MessengerServer) handleAddContact(request shared.Request) shared.Response {
	if err := ms.storage.AddContact(request.Username, request.Contact); err != nil {
		return shared.Response{Status: "error", Message: err.Error()}
	}

	if err := ms.storage.SaveContacts(); err != nil {
		return shared.Response{Status: "error", Message: err.Error()}
	}

	return shared.Response{Status: "success"}
}

func (ms *MessengerServer) handleCreateGroup(request shared.Request) shared.Response {
	groupID := fmt.Sprintf("group_%d", time.Now().UnixNano())
	group := &shared.Group{
		ID:      groupID,
		Name:    request.Name,
		Owner:   request.Username,
		Members: append([]string{request.Username}, request.Members...),
	}

	ms.storage.AddGroup(group)

	if err := ms.storage.SaveGroups(); err != nil {
		return shared.Response{Status: "error", Message: err.Error()}
	}

	return shared.Response{
		Status: "success",
		Data:   map[string]string{"group_id": groupID},
	}
}

func (ms *MessengerServer) handleSendMessage(request shared.Request) shared.Response {
	msg := shared.Message{
		ID:      time.Now().UnixNano(),
		From:    request.Username,
		To:      request.To,
		Content: request.Content,
		SentAt:  time.Now(),
		IsGroup: request.IsGroup,
		GroupID: request.GroupID,
	}

	if request.IsGroup {
		group, exists := ms.storage.GetGroup(request.GroupID)
		if !exists {
			return shared.Response{Status: "error", Message: "Group not found"}
		}

		for _, member := range group.Members {
			if member != request.Username {
				msg.To = member
				ms.storage.AddMessage(msg)
			}
		}
	} else {
		ms.storage.AddMessage(msg)
	}

	if err := ms.storage.SaveMessages(); err != nil {
		return shared.Response{Status: "error", Message: err.Error()}
	}

	return shared.Response{Status: "success"}
}

func (ms *MessengerServer) handleGetMessages(request shared.Request) shared.Response {
	messages := ms.storage.GetMessages(request.Username)

	if len(messages) > 0 {
		go ms.storage.SaveMessages()
	}

	return shared.Response{
		Status: "success",
		Data:   messages,
	}
}

func (ms *MessengerServer) handleGetContacts(request shared.Request) shared.Response {
	contacts := ms.storage.GetContacts(request.Username)
	return shared.Response{
		Status: "success",
		Data:   contacts,
	}
}
