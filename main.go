package main

import (
	"fmt"
	"log"
	"os"

	"telegraf/server"
)

func getConfig() (string, string) {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080" // значение по умолчанию для локальной разработки
	}

	environment := os.Getenv("ENVIRONMENT")
	if environment == "" {
		environment = "development"
	}

	return port, environment
}

func main() {
	port, environment := getConfig()

	fmt.Printf("🚀 Starting P2P Messenger Server...\n")
	fmt.Printf("📍 Environment: %s\n", environment)
	fmt.Printf("🔌 Port: %s\n", port)

	// Определяем хост в зависимости от окружения
	host := "localhost"
	if environment == "production" {
		host = "0.0.0.0" // слушаем все интерфейсы в продакшене
	}

	serverConfig := server.ServerConfig{
		Host: host,
		Port: port, // Используем порт из переменной окружения
	}

	storageConfig := server.Storage{
		UsersFile:    "users.dat",
		MessagesFile: "messages.dat",
		ContactsFile: "contacts.dat",
		GroupsFile:   "groups.dat",
	}

	messengerServer := server.NewMessengerServer(serverConfig, storageConfig)

	log.Printf("✅ Server configured - Host: %s, Port: %s", host, port)

	if err := messengerServer.Start(); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
