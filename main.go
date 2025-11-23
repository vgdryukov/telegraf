package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"telegraf/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	environment := os.Getenv("ENVIRONMENT")
	if environment == "" {
		environment = "production"
	}

	fmt.Printf("🚀 Starting P2P Messenger Server...\n")
	fmt.Printf("📍 Environment: %s\n", environment)
	fmt.Printf("🌐 Port: %s\n", port)

	host := "0.0.0.0"
	if environment == "development" {
		host = "localhost"
	}

	serverConfig := server.ServerConfig{
		Host: host,
		Port: port, // TCP порт
	}

	storageConfig := server.StorageConfig{
		UsersFile:    "users.dat",
		MessagesFile: "messages.dat",
		ContactsFile: "contacts.dat",
		GroupsFile:   "groups.dat",
	}

	messengerServer := server.NewMessengerServer(serverConfig, storageConfig)

	log.Printf("✅ Server configured - Host: %s, Port: %s", host, port)

	// Создаем контекст для graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Запускаем сервер
	if err := messengerServer.Start(ctx, port); err != nil {
		log.Fatalf("❌ Failed to start server: %v", err)
	}

	log.Println("👋 Server stopped")
}
