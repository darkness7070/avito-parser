package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	// Initialize Avito parser
	avitoParser := parser.NewAvitoParser(
		redisClient,
		cfg.Browser.Headless,
		cfg.Browser.Timeout,
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

	// Start parsing in a separate goroutine
	go func() {
		// URL to parse (you can modify this or pass as argument)
		url := "https://www.avito.ru/chelyabinsk/kvartiry/sdam/na_dlitelnyy_srok-ASgBAgICAkSSA8gQ8AeQUg?context=H4sIAAAAAAAA_wEjANz_YToxOntzOjg6ImZyb21QYWdlIjtzOjc6ImNhdGFsb2ciO312FITcIwAAAA&district=16"
		
		log.Println("Starting to parse Avito listings...")
		
		for {
			select {
			case <-sigChan:
				log.Println("Received shutdown signal, stopping parser...")
				return
			default:
				// Parse listings
				listings, err := avitoParser.ParseListings(url)
				if err != nil {
					log.Printf("Error parsing listings: %v", err)
				} else {
					log.Printf("Found %d listings", len(listings))
					
					// Save each listing
					for _, listing := range listings {
						err := avitoParser.SaveListing(listing)
						if err != nil {
							log.Printf("Error saving listing: %v", err)
						}
					}
				}
				
				// Wait before next parsing cycle
				log.Printf("Waiting %v before next parsing cycle...", cfg.Parser.DelayBetweenRequests)
				time.Sleep(cfg.Parser.DelayBetweenRequests)
			}
		}
	}()

	log.Println("Avito parser started. Press Ctrl+C to stop.")
	
	// Wait for shutdown signal
	<-sigChan
	log.Println("Shutting down gracefully...")
}