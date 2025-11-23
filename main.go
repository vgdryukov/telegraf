package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"telegraf/server"
)

func getConfig() (string, string, string) {
	// PORT используется Render для health checks
	httpPort := os.Getenv("PORT")
	if httpPort == "" {
		httpPort = "8080"
	}

	// TCP порт для вашего приложения - используем другой порт
	tcpPort := os.Getenv("TCP_PORT")
	if tcpPort == "" {
		// Если HTTP порт 8080, используем 10000, иначе используем порт + 1
		if httpPort == "8080" {
			tcpPort = "10000"
		} else {
			// Конвертируем HTTP порт в число и добавляем 1
			if portNum, err := strconv.Atoi(httpPort); err == nil {
				tcpPort = strconv.Itoa(portNum + 1)
			} else {
				tcpPort = "10000"
			}
		}
	}

	environment := os.Getenv("ENVIRONMENT")
	if environment == "" {
		environment = "production"
	}

	return tcpPort, httpPort, environment
}

// startHealthCheckServer запускает HTTP сервер для health checks от Render
func startHealthCheckServer(port string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" || r.Method == "GET" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  15 * time.Second,
	}

	go func() {
		log.Printf("🌐 HTTP Health Check server starting on port %s", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("❌ HTTP server error: %v", err)
		}
	}()

	return server
}

func main() {
	tcpPort, httpPort, environment := getConfig()

	fmt.Printf("🚀 Starting P2P Messenger Server...\n")
	fmt.Printf("📍 Environment: %s\n", environment)
	fmt.Printf("🔌 TCP Port: %s\n", tcpPort)
	fmt.Printf("🌐 HTTP Port: %s\n", httpPort)

	// Проверяем, что порты разные
	if tcpPort == httpPort {
		log.Fatalf("❌ Port conflict: TCP port %s cannot be the same as HTTP port", tcpPort)
	}

	host := "0.0.0.0"
	if environment == "development" {
		host = "localhost"
	}

	serverConfig := server.ServerConfig{
		Host: host,
		Port: tcpPort,
	}

	storageConfig := server.StorageConfig{
		UsersFile:    "users.dat",
		MessagesFile: "messages.dat",
		ContactsFile: "contacts.dat",
		GroupsFile:   "groups.dat",
	}

	messengerServer := server.NewMessengerServer(serverConfig, storageConfig)

	// Запускаем HTTP сервер для health checks
	healthServer := startHealthCheckServer(httpPort)

	log.Printf("✅ Server configured - Host: %s, TCP Port: %s, HTTP Port: %s", host, tcpPort, httpPort)

	// Создаем контекст для graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Запускаем основной TCP сервер
	go func() {
		if err := messengerServer.Start(ctx, httpPort); err != nil {
			log.Printf("❌ TCP server error: %v", err)
		}
	}()

	// Ожидаем сигнал завершения
	<-ctx.Done()
	log.Println("🛑 Shutdown signal received")

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Останавливаем HTTP сервер
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("❌ HTTP server shutdown error: %v", err)
	} else {
		log.Println("✅ HTTP server stopped gracefully")
	}

	log.Println("👋 Server stopped")
}
