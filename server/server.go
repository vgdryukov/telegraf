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
		return fmt.Errorf("failed to start listener on %s: %v", address, err)
	}
	defer listener.Close()

	log.Printf("🚀 P2P Messenger Server started on %s", address)
	log.Printf("📍 Host: %s, Port: %s", ms.config.Host, ms.config.Port)
	log.Printf("💾 Storage files: users=%s, messages=%s, contacts=%s, groups=%s",
		ms.storage.config.UsersFile,
		ms.storage.config.MessagesFile,
		ms.storage.config.ContactsFile,
		ms.storage.config.GroupsFile)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("❌ Accept error: %v", err)
			continue
		}
		go ms.handleConnection(conn)
	}
}

func (ms *MessengerServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	log.Printf("🔗 New connection from %s", remoteAddr)

	buffer := make([]byte, 4096)

	for {
		n, err := conn.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Printf("❌ Read error from %s: %v", remoteAddr, err)
			} else {
				log.Printf("📤 Connection closed by %s", remoteAddr)
			}

			// Удаляем пользователя из онлайн списка при отключении
			ms.mutex.Lock()
			for username, userConn := range ms.onlineUsers {
				if userConn == conn {
					delete(ms.onlineUsers, username)
					log.Printf("👤 User %s went offline", username)
					break
				}
			}
			ms.mutex.Unlock()
			return
		}

		var request shared.Request
		if err := json.Unmarshal(buffer[:n], &request); err != nil {
			log.Printf("❌ JSON unmarshal error from %s: %v", remoteAddr, err)
			response := shared.Response{Status: "error", Message: "Invalid JSON format"}
			responseData, _ := json.Marshal(response)
			conn.Write(responseData)
			continue
		}

		log.Printf("📨 Request from %s: %s (user: %s)", remoteAddr, request.Action, request.Username)
		response := ms.handleRequest(request, conn)
		responseData, _ := json.Marshal(response)

		if _, err := conn.Write(responseData); err != nil {
			log.Printf("❌ Write error to %s: %v", remoteAddr, err)
			return
		}

		log.Printf("📤 Response sent to %s: %s", remoteAddr, response.Status)
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
		log.Printf("❌ Unknown action from %s: %s", request.Username, request.Action)
		return shared.Response{Status: "error", Message: "Unknown action: " + request.Action}
	}
}

func (ms *MessengerServer) handleRegister(request shared.Request) shared.Response {
	if request.Username == "" || request.Password == "" {
		return shared.Response{Status: "error", Message: "Username and password are required"}
	}

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
		log.Printf("❌ Failed to save users: %v", err)
		return shared.Response{Status: "error", Message: "Failed to save user data"}
	}

	log.Printf("✅ New user registered: %s", request.Username)
	return shared.Response{Status: "success", Message: "Registration successful"}
}

func (ms *MessengerServer) handleLogin(request shared.Request, conn net.Conn) shared.Response {
	if request.Username == "" || request.Password == "" {
		return shared.Response{Status: "error", Message: "Username and password are required"}
	}

	user, exists := ms.storage.GetUser(request.Username)
	if !exists {
		log.Printf("❌ Login failed - user not found: %s", request.Username)
		return shared.Response{Status: "error", Message: "Invalid credentials"}
	}

	passwordHash := fmt.Sprintf("%x", sha256.Sum256([]byte(request.Password)))
	if user.Password != passwordHash {
		log.Printf("❌ Login failed - invalid password for: %s", request.Username)
		return shared.Response{Status: "error", Message: "Invalid credentials"}
	}

	ms.mutex.Lock()
	ms.onlineUsers[request.Username] = conn
	ms.mutex.Unlock()

	log.Printf("✅ User logged in: %s from %s", request.Username, conn.RemoteAddr())
	return shared.Response{Status: "success", Message: "Login successful"}
}

func (ms *MessengerServer) handleRecover(request shared.Request) shared.Response {
	if request.Username == "" || request.Email == "" {
		return shared.Response{Status: "error", Message: "Username and email are required"}
	}

	user, exists := ms.storage.GetUser(request.Username)
	if !exists {
		return shared.Response{Status: "error", Message: "User not found"}
	}

	if user.Email != request.Email {
		return shared.Response{Status: "error", Message: "Email doesn't match"}
	}

	log.Printf("🔐 Password recovery for user: %s", request.Username)
	return shared.Response{
		Status:  "success",
		Message: "Password: " + user.Password, // В реальном приложении отправили бы на email
	}
}

func (ms *MessengerServer) handleAddContact(request shared.Request) shared.Response {
	if request.Username == "" || request.Contact == "" {
		return shared.Response{Status: "error", Message: "Username and contact are required"}
	}

	if request.Username == request.Contact {
		return shared.Response{Status: "error", Message: "Cannot add yourself as contact"}
	}

	if _, exists := ms.storage.GetUser(request.Contact); !exists {
		return shared.Response{Status: "error", Message: "Contact user not found"}
	}

	if err := ms.storage.AddContact(request.Username, request.Contact); err != nil {
		return shared.Response{Status: "error", Message: err.Error()}
	}

	if err := ms.storage.SaveContacts(); err != nil {
		log.Printf("❌ Failed to save contacts: %v", err)
		return shared.Response{Status: "error", Message: "Failed to save contacts"}
	}

	log.Printf("✅ Contact added: %s -> %s", request.Username, request.Contact)
	return shared.Response{Status: "success", Message: "Contact added successfully"}
}

func (ms *MessengerServer) handleCreateGroup(request shared.Request) shared.Response {
	if request.Username == "" || request.Name == "" {
		return shared.Response{Status: "error", Message: "Group name and owner are required"}
	}

	groupID := fmt.Sprintf("group_%d", time.Now().UnixNano())
	group := &shared.Group{
		ID:      groupID,
		Name:    request.Name,
		Owner:   request.Username,
		Members: append([]string{request.Username}, request.Members...),
	}

	// Проверяем существование всех участников
	for _, member := range request.Members {
		if _, exists := ms.storage.GetUser(member); !exists {
			return shared.Response{Status: "error", Message: "User " + member + " not found"}
		}
	}

	ms.storage.AddGroup(group)

	if err := ms.storage.SaveGroups(); err != nil {
		log.Printf("❌ Failed to save groups: %v", err)
		return shared.Response{Status: "error", Message: "Failed to save group"}
	}

	log.Printf("✅ Group created: %s (ID: %s) by %s", request.Name, groupID, request.Username)
	return shared.Response{
		Status:  "success",
		Message: "Group created successfully",
		Data:    map[string]string{"group_id": groupID},
	}
}

func (ms *MessengerServer) handleSendMessage(request shared.Request) shared.Response {
	if request.Username == "" || request.Content == "" {
		return shared.Response{Status: "error", Message: "Username and content are required"}
	}

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
		if request.GroupID == "" {
			return shared.Response{Status: "error", Message: "Group ID is required for group messages"}
		}

		group, exists := ms.storage.GetGroup(request.GroupID)
		if !exists {
			return shared.Response{Status: "error", Message: "Group not found"}
		}

		// Проверяем что пользователь состоит в группе
		isMember := false
		for _, member := range group.Members {
			if member == request.Username {
				isMember = true
				break
			}
		}
		if !isMember {
			return shared.Response{Status: "error", Message: "You are not a member of this group"}
		}

		for _, member := range group.Members {
			if member != request.Username {
				msg.To = member
				ms.storage.AddMessage(msg)
			}
		}
	} else {
		if request.To == "" {
			return shared.Response{Status: "error", Message: "Recipient is required for private messages"}
		}
		ms.storage.AddMessage(msg)
	}

	if err := ms.storage.SaveMessages(); err != nil {
		log.Printf("❌ Failed to save messages: %v", err)
		return shared.Response{Status: "error", Message: "Failed to save message"}
	}

	log.Printf("✅ Message sent: %s -> %s (group: %v)", request.Username, request.To, request.IsGroup)
	return shared.Response{Status: "success", Message: "Message sent successfully"}
}

func (ms *MessengerServer) handleGetMessages(request shared.Request) shared.Response {
	if request.Username == "" {
		return shared.Response{Status: "error", Message: "Username is required"}
	}

	messages := ms.storage.GetMessages(request.Username)

	if len(messages) > 0 {
		go ms.storage.SaveMessages()
	}

	log.Printf("📨 Retrieved %d messages for user: %s", len(messages), request.Username)
	return shared.Response{
		Status: "success",
		Data:   messages,
	}
}

func (ms *MessengerServer) handleGetContacts(request shared.Request) shared.Response {
	if request.Username == "" {
		return shared.Response{Status: "error", Message: "Username is required"}
	}

	contacts := ms.storage.GetContacts(request.Username)
	log.Printf("👥 Retrieved %d contacts for user: %s", len(contacts), request.Username)
	return shared.Response{
		Status: "success",
		Data:   contacts,
	}
}
