package server

type ServerConfig struct {
	Host string `json:"host"`
	Port string `json:"port"`
}

type Storage struct {
	UsersFile    string `json:"users_file"`
	MessagesFile string `json:"messages_file"`
	ContactsFile string `json:"contacts_file"`
	GroupsFile   string `json:"groups_file"`
}
