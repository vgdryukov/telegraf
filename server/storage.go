package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"telegraf/shared"
)

type DataStorage struct {
	users           map[string]*shared.User
	undeliveredMsgs map[string][]shared.Message
	contacts        map[string][]string
	groups          map[string]*shared.Group
	mutex           sync.RWMutex
	config          Storage
}

func NewDataStorage(config Storage) *DataStorage {
	return &DataStorage{
		users:           make(map[string]*shared.User),
		undeliveredMsgs: make(map[string][]shared.Message),
		contacts:        make(map[string][]string),
		groups:          make(map[string]*shared.Group),
		config:          config,
	}
}

func (ds *DataStorage) hashData(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:]
}

func (ds *DataStorage) loadEncryptedFile(filename string) ([]byte, error) {
	encrypted, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return []byte{}, nil
		}
		return nil, err
	}

	if len(encrypted) < sha256.Size {
		return encrypted, nil
	}

	return encrypted[sha256.Size:], nil
}

func (ds *DataStorage) saveEncryptedFile(filename string, data []byte) error {
	hash := ds.hashData(data)
	encrypted := append(hash, data...)
	return os.WriteFile(filename, encrypted, 0644)
}

func (ds *DataStorage) LoadAll() error {
	if err := ds.loadUsers(); err != nil {
		return err
	}
	if err := ds.loadMessages(); err != nil {
		return err
	}
	if err := ds.loadContacts(); err != nil {
		return err
	}
	return ds.loadGroups()
}

func (ds *DataStorage) loadUsers() error {
	data, err := ds.loadEncryptedFile(ds.config.UsersFile)
	if err != nil {
		return err
	}

	if len(data) == 0 {
		return nil
	}

	var users []shared.User
	if err := json.Unmarshal(data, &users); err != nil {
		return err
	}

	for i := range users {
		ds.users[users[i].Username] = &users[i]
	}
	return nil
}

func (ds *DataStorage) SaveUsers() error {
	users := make([]shared.User, 0, len(ds.users))
	for _, user := range ds.users {
		users = append(users, *user)
	}

	data, err := json.Marshal(users)
	if err != nil {
		return err
	}

	return ds.saveEncryptedFile(ds.config.UsersFile, data)
}

func (ds *DataStorage) loadMessages() error {
	data, err := ds.loadEncryptedFile(ds.config.MessagesFile)
	if err != nil {
		return err
	}

	if len(data) == 0 {
		return nil
	}

	var messages []shared.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		return err
	}

	for _, msg := range messages {
		ds.undeliveredMsgs[msg.To] = append(ds.undeliveredMsgs[msg.To], msg)
	}
	return nil
}

func (ds *DataStorage) SaveMessages() error {
	var allMessages []shared.Message
	for _, messages := range ds.undeliveredMsgs {
		allMessages = append(allMessages, messages...)
	}

	data, err := json.Marshal(allMessages)
	if err != nil {
		return err
	}

	return ds.saveEncryptedFile(ds.config.MessagesFile, data)
}

func (ds *DataStorage) loadContacts() error {
	data, err := os.ReadFile(ds.config.ContactsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if len(data) == 0 {
		return nil
	}

	return json.Unmarshal(data, &ds.contacts)
}

func (ds *DataStorage) SaveContacts() error {
	data, err := json.Marshal(ds.contacts)
	if err != nil {
		return err
	}

	return os.WriteFile(ds.config.ContactsFile, data, 0644)
}

func (ds *DataStorage) loadGroups() error {
	data, err := os.ReadFile(ds.config.GroupsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if len(data) == 0 {
		return nil
	}

	return json.Unmarshal(data, &ds.groups)
}

func (ds *DataStorage) SaveGroups() error {
	data, err := json.Marshal(ds.groups)
	if err != nil {
		return err
	}

	return os.WriteFile(ds.config.GroupsFile, data, 0644)
}

// Геттеры с синхронизацией
func (ds *DataStorage) GetUser(username string) (*shared.User, bool) {
	ds.mutex.RLock()
	defer ds.mutex.RUnlock()
	user, exists := ds.users[username]
	return user, exists
}

func (ds *DataStorage) AddUser(user *shared.User) {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()
	ds.users[user.Username] = user
	ds.contacts[user.Username] = []string{}
}

func (ds *DataStorage) AddMessage(msg shared.Message) {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()
	ds.undeliveredMsgs[msg.To] = append(ds.undeliveredMsgs[msg.To], msg)
}

func (ds *DataStorage) GetMessages(username string) []shared.Message {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()
	messages := ds.undeliveredMsgs[username]
	delete(ds.undeliveredMsgs, username)
	return messages
}

func (ds *DataStorage) AddContact(username, contact string) error {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()

	if _, exists := ds.users[contact]; !exists {
		return fmt.Errorf("user %s not found", contact)
	}

	for _, existing := range ds.contacts[username] {
		if existing == contact {
			return fmt.Errorf("contact already exists")
		}
	}

	ds.contacts[username] = append(ds.contacts[username], contact)
	return nil
}

func (ds *DataStorage) GetContacts(username string) []string {
	ds.mutex.RLock()
	defer ds.mutex.RUnlock()
	return ds.contacts[username]
}

func (ds *DataStorage) AddGroup(group *shared.Group) {
	ds.mutex.Lock()
	defer ds.mutex.Unlock()
	ds.groups[group.ID] = group
}

func (ds *DataStorage) GetGroup(groupID string) (*shared.Group, bool) {
	ds.mutex.RLock()
	defer ds.mutex.RUnlock()
	group, exists := ds.groups[groupID]
	return group, exists
}
