package storage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/bson"
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

// MangoStore manages MongoDB message persistence.
type MangoStore struct {
	Client     *mango.Client
	Collection *mango.Collection
}

// NewMangoStore connects to MongoDB, pings, and ensures indexes exist.
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

	coll := client.Database(dbName).Collection("messages")

	// Create indexes for query performance
	indexCtx, indexCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer indexCancel()
	_, _ = coll.Indexes().CreateMany(indexCtx, []mango.IndexModel{
		{Keys: bson.D{{Key: "room_id", Value: 1}, {Key: "timestamp", Value: -1}}},
		{Keys: bson.D{{Key: "to", Value: 1}, {Key: "delivered", Value: 1}}},
	})

	return &MangoStore{
		Client:     client,
		Collection: coll,
	}, nil
}

// Close disconnects from MongoDB.
func (ms *MangoStore) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ms.Client.Disconnect(ctx)
}

// SaveMessage persists a chat message. Generates an ID if empty.
func (ms *MangoStore) SaveMessage(msg *ChatMessage) error {
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("%d_%s", time.Now().UnixNano(), msg.From)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := ms.Collection.InsertOne(ctx, msg)
	if err != nil {
		return fmt.Errorf("save message: %w", err)
	}
	return nil
}

// QueryHistory returns recent room messages, sorted by timestamp descending.
func (ms *MangoStore) QueryHistory(roomID string, limit int64) ([]*ChatMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cur, err := ms.Collection.Find(ctx,
		map[string]any{"room_id": roomID},
		options.Find().SetSort(map[string]int{"timestamp": -1}).SetLimit(limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer cur.Close(ctx)

	var msgs []*ChatMessage
	for cur.Next(ctx) {
		var m ChatMessage
		if err := cur.Decode(&m); err != nil {
			slog.Warn("decode history message", "error", err)
			continue
		}
		msgs = append(msgs, &m)
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("history cursor: %w", err)
	}
	return msgs, nil
}

// PullUndeliveredForUser fetches undelivered private messages and marks them delivered.
// Returns up to 200 messages per call to bound memory usage.
func (ms *MangoStore) PullUndeliveredForUser(userID string) ([]*ChatMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	filter := map[string]any{"to": userID, "delivered": false}
	opts := options.Find().SetLimit(200)
	cur, err := ms.Collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("find undelivered: %w", err)
	}
	defer cur.Close(ctx)

	var msgs []*ChatMessage
	for cur.Next(ctx) {
		var m ChatMessage
		if err := cur.Decode(&m); err != nil {
			slog.Warn("decode undelivered message", "error", err)
			continue
		}
		msgs = append(msgs, &m)
	}
	if err := cur.Err(); err != nil {
		return nil, fmt.Errorf("undelivered cursor: %w", err)
	}

	// Mark as delivered — return error on failure to prevent duplicate delivery
	if len(msgs) > 0 {
		if _, err := ms.Collection.UpdateMany(ctx, filter,
			map[string]any{"$set": map[string]any{"delivered": true}}); err != nil {
			return nil, fmt.Errorf("mark delivered: %w", err)
		}
	}

	return msgs, nil
}
