package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/hyperbolic2346/gatehouse/internal/db"
	"github.com/hyperbolic2346/gatehouse/internal/frigate"
	"github.com/hyperbolic2346/gatehouse/internal/gate"
	mqttpkg "github.com/hyperbolic2346/gatehouse/internal/mqtt"
	"github.com/hyperbolic2346/gatehouse/internal/server"
	"github.com/hyperbolic2346/gatehouse/internal/ws"
)

func main() {
	cfg := server.Config{
		Port:        envInt("PORT", 8080),
		FrontendDir: envStr("FRONTEND_DIR", "../frontend/build"),
		JWTSecret:   envStr("JWT_SECRET", ""),
		FrigateURL:  envStr("FRIGATE_URL", "http://frigate.home.svc.cluster.local:5000"),
		MQTTBroker:  envStr("MQTT_BROKER", "tcp://mosquitto.home.svc.cluster.local:8883"),
		DBPath:      envStr("DB_PATH", "gatehouse.db"),
	}

	if cfg.JWTSecret == "" {
		log.Fatal("JWT_SECRET environment variable is required")
	}

	// Open database (runs migrations and seeds admin user if needed).
	database, err := db.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Graceful shutdown on SIGINT or SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Create Frigate client.
	frigateClient := frigate.New(cfg.FrigateURL)

	// Create WebSocket hub and start its run loop.
	hub := ws.NewHub()
	go hub.Run()

	// Create gate controller and MQTT subscriber. The MQTT client will be
	// set once the connection is established (in the background).
	gateCtrl := gate.New(nil, hub)
	mqttSub := mqttpkg.New(nil, hub, frigateClient)

	// Connect MQTT in the background so the HTTP server can start
	// immediately and pass health probes while waiting for the broker.
	go func() {
		mqttClient, err := mqttpkg.Connect(cfg.MQTTBroker, func(_ pahomqtt.Client) {
			// OnConnect fires on initial connect and every reconnect.
			mqttSub.Subscribe()
			gateCtrl.Subscribe()
		})
		if err != nil {
			log.Printf("MQTT connect failed: %v", err)
			return
		}

		// Update the subscriber and controller with the live client.
		mqttSub.SetClient(mqttClient)
		gateCtrl.SetClient(mqttClient)

		// Trigger initial subscriptions.
		mqttSub.Subscribe()
		gateCtrl.Subscribe()

		log.Println("MQTT connected and subscriptions registered")

		// Block until shutdown, then disconnect.
		<-ctx.Done()
		log.Println("MQTT disconnecting")
		mqttClient.Disconnect(250)
	}()

	// Start day-rollover timer.
	go dayRolloverLoop(hub)

	// Create and start HTTP server.
	srv := server.New(cfg, database, hub, frigateClient, gateCtrl)

	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("HTTP server stopped: %v", err)
		}
	}()

	log.Printf("Gatehouse started on port %d", cfg.Port)

	// Block until we receive a termination signal.
	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	database.Close()
	log.Println("Goodbye.")
}

// dayRolloverLoop sleeps until midnight, then broadcasts a day_rollover event
// to all WebSocket clients. It repeats every 24 hours.
func dayRolloverLoop(hub *ws.Hub) {
	for {
		now := time.Now()
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		time.Sleep(time.Until(nextMidnight))

		hub.Broadcast([]byte(`{"type":"day_rollover"}`))
		log.Println("Day rollover event broadcast")
	}
}

// envStr reads a string environment variable with a fallback default.
func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envInt reads an integer environment variable with a fallback default.
func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			log.Printf("Warning: invalid integer for %s=%q, using default %d", key, v, fallback)
			return fallback
		}
		return n
	}
	return fallback
}
