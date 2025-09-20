package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"avito-parser/internal/config"
	"avito-parser/internal/database"
	"avito-parser/internal/parser"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize Redis client
	redisClient, err := database.NewRedisClient(
		cfg.Redis.Host,
		cfg.Redis.Port,
		cfg.Redis.Password,
		cfg.Redis.DB,
	)
	if err != nil {
		log.Fatalf("Failed to initialize Redis client: %v", err)
	}
	defer redisClient.Close()

	// Initialize Avito parser with new parameters
	avitoParser := parser.NewAvitoParser(
		redisClient,
		cfg.Browser.Headless,
		cfg.Browser.Timeout,
		cfg.Avito.BaseURL,
		cfg.Parser.CycleDelay,
		cfg.Parser.PageDelay,
	)

	// Start browser
	err = avitoParser.Start()
	if err != nil {
		log.Fatalf("Failed to start parser: %v", err)
	}
	defer avitoParser.Close()

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start continuous parsing in a separate goroutine
	go func() {
		log.Println("Starting continuous multi-page parsing...")
		avitoParser.StartContinuousParsing()
	}()

	log.Println("Avito multi-page parser started. Press Ctrl+C to stop.")
	
	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down gracefully...")
}