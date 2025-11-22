package shared

import "time"

type User struct {
	Username string `json:"username"`
	Password string `json:"password"` // хранится как sha256
	Email    string `json:"email,omitempty"`
}

type Message struct {
	ID      int64     `json:"id"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Content string    `json:"content"`
	SentAt  time.Time `json:"sent_at"`
	IsGroup bool      `json:"is_group"`
	GroupID string    `json:"group_id,omitempty"`
}

type Group struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Owner   string   `json:"owner"`
	Members []string `json:"members"`
}

type Request struct {
	Action   string      `json:"action"`
	Username string      `json:"username,omitempty"`
	Password string      `json:"password,omitempty"`
	Email    string      `json:"email,omitempty"`
	Contact  string      `json:"contact,omitempty"`
	To       string      `json:"to,omitempty"`
	Content  string      `json:"content,omitempty"`
	IsGroup  bool        `json:"is_group,omitempty"`
	GroupID  string      `json:"group_id,omitempty"`
	Name     string      `json:"name,omitempty"`
	Members  []string    `json:"members,omitempty"`
	Data     interface{} `json:"data,omitempty"`
}

type Response struct {
	Status  string      `json:"status"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}
