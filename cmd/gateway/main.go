package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spf13/viper"
	"github.com/xh1126xx/gochatx/internal/gateway"
	"github.com/xh1126xx/gochatx/internal/storage"
)

func loadConfig(path string) {
	viper.SetDefault("authsvc.addr", "localhost:50051")
	viper.SetDefault("mongo.uri", "mongodb://127.0.0.1:27017")
	viper.SetDefault("mongo.db", "gochatx")
	viper.SetDefault("redis.addr", "127.0.0.1:6379")
	viper.SetDefault("gateway.listen", ":8080")

	if path != "" {
		viper.SetConfigFile(path)
		if err := viper.ReadInConfig(); err != nil {
			log.Printf("read config err: %v, using defaults", err)
		}
	}
}

func main() {
	cfg := flag.String("config", "config.yaml", "config file path (optional)")
	flag.Parse()

	loadConfig(*cfg)

	// optional connect to auth svc
	authAddr := viper.GetString("authsvc.addr")
	var authConn *grpc.ClientConn
	if authAddr != "" {
		ctx, cancel := contextWithTimeout(3 * time.Second)
		conn, err := grpc.DialContext(ctx, authAddr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
		cancel()
		if err != nil {
			log.Printf("warning: can't connect to auth svc %s: %v (fall back to token-as-userID mode)", authAddr, err)
			authConn = nil
		} else {
			authConn = conn
			log.Println("connected to auth service", authAddr)
		}
	}

	var mongoStore *storage.MangoStore
	mongoURI := viper.GetString("mongo.uri")
	mongoDB := viper.GetString("mongo.db")
	if mongoURI != "" {
		mongoStore = storage.NewMangoStore(mongoURI, mongoDB)
	}

	// init redis
	var rdb *redis.Client
	redisAddr := viper.GetString("redis.addr")
	if redisAddr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
		// quick ping
		pctx, pcancel := contextWithTimeout(2 * time.Second)
		if err := rdb.Ping(pctx).Err(); err != nil {
			log.Printf("warning: redis ping failed: %v (continuing)", err)
		}
		pcancel()
	}

	// gateway server
	gw := gateway.NewGatewayServer(authConn, mongoStore, rdb)

	// serve static web and ws
	http.HandleFunc("/ws", gw.HandleWS)
	http.Handle("/", http.FileServer(http.Dir("./web")))

	addr := viper.GetString("gateway.listen")
	log.Println("Gateway listening on", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

// helper context with timeout
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
