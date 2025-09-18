package storage

import (
	"context"
	"log"
	"time"

	mango "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ChatMessage struct {
	ID        string    `bson:"_id"`
	From      string    `bson:"from"`
	To        string    `bson:"to"`
	RoomID    string    `bson:"room_id"`
	Content   string    `bson:"content"`
	Timestamp time.Time `bson:"timestamp"`
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

func (ms *MangoStore) SaveMessage(msg ChatMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := ms.Collection.InsertOne(ctx, msg)
	return err
}
