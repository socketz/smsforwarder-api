package store

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	Status       string
	CreatedAt    int64
}

type Device struct {
	ID        int64
	UserID    int64
	TokenHash string
	Label     string
	CreatedAt int64
}

type Channel struct {
	ID        int64
	UserID    int64
	Type      string
	Enabled   bool
	Config    string // raw JSON
	CreatedAt int64
	UpdatedAt int64
}

type OutboxItem struct {
	ID          int64
	UserID      int64
	ChannelID   int64
	Sender      string
	Content     string
	SmsTs       int64
	ClientMsgID string
	Attempts    int
	NextAt      int64
	CreatedAt   int64
}

type DeliveryLog struct {
	ID          int64
	UserID      int64
	ChannelType string
	Sender      string
	Status      string
	Detail      string
	CreatedAt   int64
}
