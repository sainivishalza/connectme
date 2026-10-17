package database

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// Mongo holds the client and the default database (taken from the URL path).
type Mongo struct {
	Client *mongo.Client
	DB     *mongo.Database
}

func NewMongo(ctx context.Context, uri string) (*Mongo, error) {
	opts := options.Client().ApplyURI(uri).SetServerSelectionTimeout(5 * time.Second)
	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, fmt.Errorf("connect mongo: %w", err)
	}
	m := &Mongo{Client: client, DB: client.Database(DBName(uri))}
	if err := m.Ping(ctx); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	return m, nil
}

func (m *Mongo) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := m.Client.Ping(ctx, readpref.Primary()); err != nil {
		return fmt.Errorf("ping mongo: %w", err)
	}
	return nil
}

func (m *Mongo) Collection(name string) *mongo.Collection { return m.DB.Collection(name) }

func (m *Mongo) Close(ctx context.Context) error { return m.Client.Disconnect(ctx) }

// DBName returns the database in mongodb://host/<db>?opts, defaulting to "connectme".
func DBName(uri string) string {
	if u, err := url.Parse(uri); err == nil {
		if db := strings.Trim(u.Path, "/"); db != "" {
			return db
		}
	}
	return "connectme"
}
