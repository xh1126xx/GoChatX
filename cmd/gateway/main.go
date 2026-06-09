package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

	// connect to auth service
	authAddr := viper.GetString("authsvc.addr")
	var authConn *grpc.ClientConn
	if authAddr != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, err := grpc.DialContext(ctx, authAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		)
		cancel()
		if err != nil {
			log.Printf("warning: can't connect to auth service %s: %v (fall back to token-as-userID mode)", authAddr, err)
			authConn = nil
		} else {
			authConn = conn
			defer conn.Close()
			log.Println("connected to auth service", authAddr)
		}
	}

	// connect to MongoDB
	var mongoStore *storage.MangoStore
	mongoURI := viper.GetString("mongo.uri")
	mongoDB := viper.GetString("mongo.db")
	if mongoURI != "" {
		var err error
		mongoStore, err = storage.NewMangoStore(mongoURI, mongoDB)
		if err != nil {
			log.Printf("warning: can't connect to mongo: %v (message persistence disabled)", err)
			mongoStore = nil
		} else {
			defer mongoStore.Close()
		}
	}

	// connect to Redis
	var rdb *redis.Client
	redisAddr := viper.GetString("redis.addr")
	if redisAddr != "" {
		rdb = redis.NewClient(&redis.Options{Addr: redisAddr})
		pctx, pcancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := rdb.Ping(pctx).Err(); err != nil {
			log.Printf("warning: redis ping failed: %v (continuing without redis)", err)
			rdb = nil
		}
		pcancel()
		if rdb != nil {
			defer rdb.Close()
		}
	}

	// gateway server
	gw := gateway.NewGatewayServer(authConn, mongoStore, rdb)
	rest := gateway.NewRESTHandler(authConn, rdb)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", gw.HandleWS)
	mux.HandleFunc("/api/register", rest.Register)
	mux.HandleFunc("/api/login", rest.Login)
	mux.HandleFunc("/api/users/online", rest.OnlineUsers)
	mux.Handle("/", http.FileServer(http.Dir("./web")))

	addr := viper.GetString("gateway.listen")
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("shutting down gateway...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("gateway shutdown error: %v", err)
		}
	}()

	log.Println("Gateway listening on", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("gateway failed: %v", err)
	}
	log.Println("gateway stopped")
}
