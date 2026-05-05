package storage

import (
	"context"
	"fmt"
	"time"

	mango "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ChatMessage represents a chat message supporting group and private chat.
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

func NewMangoStore(uri, dbName string) (*MangoStore, error) {
	client, err := mango.Connect(context.Background(), options.Client().ApplyURI(uri).
		SetConnectTimeout(10*time.Second).
		SetServerSelectionTimeout(5*time.Second))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	return &MangoStore{
		Client:     client,
		Collection: client.Database(dbName).Collection("messages"),
	}, nil
}

// Close disconnects from MongoDB.
func (ms *MangoStore) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ms.Client.Disconnect(ctx)
}

// SaveMessage persists a chat message.
func (ms *MangoStore) SaveMessage(msg *ChatMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := ms.Collection.InsertOne(ctx, msg)
	return err
}

// QueryHistory returns recent room messages.
func (ms *MangoStore) QueryHistory(roomID string, limit int64) ([]*ChatMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cur, err := ms.Collection.Find(ctx,
		map[string]any{"room_id": roomID},
		options.Find().SetSort(map[string]int{"timestamp": -1}).SetLimit(limit),
	)
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

// PullUndeliveredForUser fetches and marks delivered private messages for a user.
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

	// mark as delivered
	_, _ = ms.Collection.UpdateMany(ctx, filter,
		map[string]any{"$set": map[string]any{"delivered": true}})

	return msgs, nil
}
