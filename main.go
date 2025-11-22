package main

import (
	"log"

	"telegraf/server"
)

func main() {
	serverConfig := server.ServerConfig{
		Host: "localhost",
		Port: "8080",
	}

	storageConfig := server.Storage{
		UsersFile:    "users.dat",
		MessagesFile: "messages.dat",
		ContactsFile: "contacts.dat",
		GroupsFile:   "groups.dat",
	}

	messengerServer := server.NewMessengerServer(serverConfig, storageConfig)

	if err := messengerServer.Start(); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}
