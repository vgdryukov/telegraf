package server

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"telegraf/shared"
	"time"
)

type MessengerServer struct {
	storage     Storage
	config      ServerConfig
	onlineUsers map[string]net.Conn
	mutex       sync.RWMutex
}

func NewMessengerServer(config ServerConfig, storageConfig StorageConfig) *MessengerServer {
	storage := NewDataStorage(storageConfig)
	return &MessengerServer{
		storage:     storage,
		config:      config,
		onlineUsers: make(map[string]net.Conn),
	}
}

func (ms *MessengerServer) Start(ctx context.Context, httpPort string) error {
	if err := ms.storage.LoadAll(); err != nil {
		return fmt.Errorf("failed to load data: %v", err)
	}

	tcpAddress := fmt.Sprintf("%s:%s", ms.config.Host, ms.config.Port)
	httpAddress := fmt.Sprintf("%s:%s", ms.config.Host, httpPort)

	log.Printf("🚀 P2P Messenger Server starting...")
	log.Printf("📍 Host: %s", ms.config.Host)
	log.Printf("🔌 TCP Server: %s", tcpAddress)
	log.Printf("🌐 HTTP Server: http://%s", httpAddress)

	// Запускаем HTTP сервер для веб-клиента в отдельной горутине
	go ms.startHTTPServer(httpAddress)

	// Основной TCP сервер для десктопного клиента
	listener, err := net.Listen("tcp", tcpAddress)
	if err != nil {
		return fmt.Errorf("failed to start TCP listener on %s: %v", tcpAddress, err)
	}
	defer listener.Close()

	// Канал для graceful shutdown
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Запускаем обработку TCP соединений в отдельной горутине
	go ms.acceptTCPConnections(serverCtx, listener)

	// Ожидаем сигнал завершения
	<-serverCtx.Done()
	log.Println("🛑 Server shutting down...")

	// Закрываем все активные соединения
	ms.closeAllConnections()

	return nil
}

// Обработка TCP соединений
func (ms *MessengerServer) acceptTCPConnections(ctx context.Context, listener net.Listener) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Устанавливаем таймаут для Accept чтобы можно было проверить контекст
			listener.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))
			conn, err := listener.Accept()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// Таймаут - проверяем контекст и продолжаем
					continue
				}
				if ctx.Err() != nil {
					// Контекст отменен - выходим
					return
				}
				log.Printf("❌ TCP Accept error: %v", err)
				continue
			}
			go ms.handleTCPConnection(conn)
		}
	}
}

// Закрытие всех активных соединений
func (ms *MessengerServer) closeAllConnections() {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	for username, conn := range ms.onlineUsers {
		conn.Close()
		log.Printf("🔌 Closed connection for user: %s", username)
	}
	ms.onlineUsers = make(map[string]net.Conn)
}

// HTTP сервер
func (ms *MessengerServer) startHTTPServer(address string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", ms.handleHTTPRoot)
	mux.HandleFunc("/api", ms.handleHTTPApi)
	mux.HandleFunc("/health", ms.handleHealthCheck)

	server := &http.Server{
		Addr:    address,
		Handler: mux,
	}

	log.Printf("🌐 HTTP server starting on http://%s", address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("❌ HTTP server error: %v", err)
	}
}

// Health check для Render
func (ms *MessengerServer) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	ms.setCORSHeaders(w)

	if r.Method == "HEAD" || r.Method == "GET" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// Обработчик корневого пути HTTP
func (ms *MessengerServer) handleHTTPRoot(w http.ResponseWriter, r *http.Request) {
	ms.setCORSHeaders(w)

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	response := map[string]string{
		"status":  "success",
		"message": "P2P Messenger Server is running",
		"version": "2.0.0",
		"api":     "Use POST /api with JSON body",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// Обработчик API запросов HTTP
func (ms *MessengerServer) handleHTTPApi(w http.ResponseWriter, r *http.Request) {
	ms.setCORSHeaders(w)

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, `{"status":"error","message":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"status":"error","message":"Error reading request body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	log.Printf("📨 HTTP Request: %s", string(body))

	var request shared.Request
	if err := json.Unmarshal(body, &request); err != nil {
		log.Printf("❌ HTTP JSON error: %v", err)
		http.Error(w, `{"status":"error","message":"Invalid JSON format"}`, http.StatusBadRequest)
		return
	}

	log.Printf("🔍 HTTP Action: %s, User: %s", request.Action, request.Username)

	response := ms.handleRequest(request, &fakeConn{})

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("❌ HTTP Response error: %v", err)
		http.Error(w, `{"status":"error","message":"Internal server error"}`, http.StatusInternalServerError)
		return
	}

	log.Printf("📤 HTTP Response: %s", response.Status)
}

// Установка CORS заголовков
func (ms *MessengerServer) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// Обработчик TCP соединений
func (ms *MessengerServer) handleTCPConnection(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	log.Printf("🔗 TCP Connection from %s", remoteAddr)

	// Устанавливаем таймауты
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetWriteDeadline(time.Now().Add(30 * time.Second))

	reader := bufio.NewReader(conn)

	for {
		// Читаем данные до новой строки
		data, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("❌ TCP Read error from %s: %v", remoteAddr, err)
			} else {
				log.Printf("📤 TCP Connection closed by %s", remoteAddr)
			}

			ms.removeUserFromOnline(conn)
			return
		}

		// Убираем символ новой строки
		if len(data) > 0 && data[len(data)-1] == '\n' {
			data = data[:len(data)-1]
		}
		if len(data) > 0 && data[len(data)-1] == '\r' {
			data = data[:len(data)-1]
		}

		// Пропускаем пустые строки
		if len(data) == 0 {
			continue
		}

		log.Printf("📨 TCP Raw data from %s: %s", remoteAddr, string(data))

		var request shared.Request
		if err := json.Unmarshal(data, &request); err != nil {
			log.Printf("❌ TCP JSON error from %s: %v", remoteAddr, err)
			response := shared.Response{Status: "error", Message: "Invalid JSON format"}
			ms.sendTCPResponse(conn, response)
			continue
		}

		log.Printf("📨 TCP Request from %s: %s (user: %s)", remoteAddr, request.Action, request.Username)
		response := ms.handleRequest(request, conn)
		ms.sendTCPResponse(conn, response)

		// Обновляем таймауты
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	}
}

// Отправка ответа по TCP
func (ms *MessengerServer) sendTCPResponse(conn net.Conn, response shared.Response) {
	responseData, _ := json.Marshal(response)
	responseData = append(responseData, '\n')

	if _, err := conn.Write(responseData); err != nil {
		log.Printf("❌ TCP Write error: %v", err)
	}

	log.Printf("📤 TCP Response sent: %s", response.Status)
}

// Удаление пользователя из онлайн списка
func (ms *MessengerServer) removeUserFromOnline(conn net.Conn) {
	ms.mutex.Lock()
	defer ms.mutex.Unlock()

	for username, userConn := range ms.onlineUsers {
		if userConn == conn {
			delete(ms.onlineUsers, username)
			log.Printf("👤 User %s went offline", username)
			break
		}
	}
}

// Fake connection для HTTP запросов
type fakeConn struct{}

func (f *fakeConn) Read(b []byte) (n int, err error)   { return 0, io.EOF }
func (f *fakeConn) Write(b []byte) (n int, err error)  { return len(b), nil }
func (f *fakeConn) Close() error                       { return nil }
func (f *fakeConn) LocalAddr() net.Addr                { return &net.TCPAddr{} }
func (f *fakeConn) RemoteAddr() net.Addr               { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0} }
func (f *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (f *fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakeConn) SetWriteDeadline(t time.Time) error { return nil }

// Основной обработчик запросов (общий для HTTP и TCP)
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

	if _, exists := ms.storage.GetUser(request.Username); exists {
		return shared.Response{Status: "error", Message: "Username already exists"}
	}

	passwordHash := fmt.Sprintf("%x", sha256.Sum256([]byte(request.Password)))
	user := &shared.User{
		Username:  request.Username,
		Password:  passwordHash,
		Email:     request.Email,
		CreatedAt: time.Now(),
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

	// Обновляем время последнего входа
	user.LastLoginAt = time.Now()
	ms.storage.SaveUsers()

	// Добавляем в онлайн пользователей только для TCP соединений
	if realConn, ok := conn.(*net.TCPConn); ok {
		ms.mutex.Lock()
		ms.onlineUsers[request.Username] = realConn
		ms.mutex.Unlock()
		log.Printf("✅ User logged in via TCP: %s from %s", request.Username, conn.RemoteAddr())
	} else {
		log.Printf("✅ User logged in via HTTP: %s", request.Username)
	}

	return shared.Response{Status: "success", Message: "Login successful"}
}

func (ms *MessengerServer) handleRecover(request shared.Request) shared.Response {
	if request.Username == "" || request.Email == "" {
		return shared.Response{Status: "error", Message: "Username and email are required"}
	}

	user, exists := ms.storage.GetUser(request.Username)
	if !exists {
		// Не раскрываем информацию о существовании пользователя
		log.Printf("🔐 Password recovery attempted for non-existent user: %s", request.Username)
		return shared.Response{Status: "success", Message: "If the user exists, recovery instructions have been sent"}
	}

	if user.Email != request.Email {
		log.Printf("🔐 Password recovery email mismatch for user: %s", request.Username)
		return shared.Response{Status: "success", Message: "If the user exists, recovery instructions have been sent"}
	}

	log.Printf("🔐 Password recovery for user: %s", request.Username)
	return shared.Response{
		Status:  "success",
		Message: "Password recovery instructions have been sent to your email",
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
		ID:        groupID,
		Name:      request.Name,
		Owner:     request.Username,
		Members:   append([]string{request.Username}, request.Members...),
		CreatedAt: time.Now(),
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

	baseTime := time.Now().UnixNano()

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

		// Создаем отдельное сообщение для каждого участника с уникальным ID
		for i, member := range group.Members {
			if member != request.Username {
				msg := shared.Message{
					ID:        baseTime + int64(i),
					From:      request.Username,
					To:        member,
					Content:   request.Content,
					SentAt:    time.Now(),
					IsGroup:   true,
					GroupID:   request.GroupID,
					Delivered: false,
				}
				ms.storage.AddMessage(msg)
			}
		}
	} else {
		if request.To == "" {
			return shared.Response{Status: "error", Message: "Recipient is required for private messages"}
		}

		msg := shared.Message{
			ID:        baseTime,
			From:      request.Username,
			To:        request.To,
			Content:   request.Content,
			SentAt:    time.Now(),
			IsGroup:   false,
			Delivered: false,
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
