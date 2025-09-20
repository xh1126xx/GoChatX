package storage

import (
	"context"
	"log"
	"time"

	mango "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 聊天消息结构体，支持群聊和私聊
type ChatMessage struct {
	ID        string    `bson:"_id"`
	From      string    `bson:"from"`
	To        string    `bson:"to,omitempty"`
	RoomID    string    `bson:"room_id,omitempty"`
	Content   string    `bson:"content"`
	Timestamp time.Time `bson:"timestamp"`
	Delivered bool      `bson:"delivered"`
}

type MangoStore struct {
	Client     *mango.Client
	Collection *mango.Collection
}

func NewMangoStore(uri, dbName string) *MangoStore {
	client, err := mango.NewClient(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		log.Fatal(err)
	}

	return &MangoStore{
		Client:     client,
		Collection: client.Database(dbName).Collection("messages"),
	}
}

// 保存消息（指针参数）
func (ms *MangoStore) SaveMessage(msg *ChatMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := ms.Collection.InsertOne(ctx, msg)
	return err
}

// 查询房间历史消息
func (ms *MangoStore) QueryHistory(roomID string, limit int64) ([]*ChatMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cur, err := ms.Collection.Find(ctx, map[string]any{"room_id": roomID}, options.Find().SetSort(map[string]int{"timestamp": -1}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var msgs []*ChatMessage
	for cur.Next(ctx) {
		var m ChatMessage
		if err := cur.Decode(&m); err == nil {
			msgs = append(msgs, &m)
		}
	}
	return msgs, nil
}

// 拉取未送达私聊消息
func (ms *MangoStore) PullUndeliveredForUser(userID string) ([]*ChatMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	filter := map[string]any{"to": userID, "delivered": false}
	cur, err := ms.Collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var msgs []*ChatMessage
	for cur.Next(ctx) {
		var m ChatMessage
		if err := cur.Decode(&m); err == nil {
			msgs = append(msgs, &m)
		}
	}
	// 标记为已送达
	_, _ = ms.Collection.UpdateMany(ctx, filter, map[string]any{"$set": map[string]any{"delivered": true}})
	return msgs, nil
}
