package parser

import (
	"fmt"
	"log"
	"strings"
	"time"

	"avito-parser/internal/database"
	"avito-parser/internal/models"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type AvitoParser struct {
	browser *rod.Browser
	db      *database.RedisClient
	headless bool
	timeout  time.Duration
}

// NewAvitoParser creates a new Avito parser instance
func NewAvitoParser(db *database.RedisClient, headless bool, timeout time.Duration) *AvitoParser {
	return &AvitoParser{
		db:      db,
		headless: headless,
		timeout: timeout,
	}
}

// Start initializes the browser
func (p *AvitoParser) Start() error {
	var l *launcher.Launcher
	if p.headless {
		l = launcher.New().Headless(true).NoSandbox(true)
	} else {
		l = launcher.New().Headless(false)
	}

	url, err := l.Launch()
	if err != nil {
		return fmt.Errorf("failed to launch browser: %w", err)
	}

	p.browser = rod.New().ControlURL(url)
	err = p.browser.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to browser: %w", err)
	}

	log.Println("Browser started successfully")
	return nil
}

// ParseListings parses apartment listings from the given URL
func (p *AvitoParser) ParseListings(url string) ([]*models.Listing, error) {
	page, err := p.browser.Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %w", err)
	}
	defer page.Close()

	// Wait for page to load
	err = page.WaitLoad()
	if err != nil {
		return nil, fmt.Errorf("failed to wait for page load: %w", err)
	}

	// Wait for listings to appear
	err = page.Timeout(p.timeout).WaitElementsMoreThan("[data-marker='item']", 0)
	if err != nil {
		log.Printf("Warning: No listings found on page: %v", err)
		return []*models.Listing{}, nil
	}

	// Find all listing elements
	listingElements, err := page.Elements("[data-marker='item']")
	if err != nil {
		return nil, fmt.Errorf("failed to find listing elements: %w", err)
	}

	var listings []*models.Listing

	for i, element := range listingElements {
		listing, err := p.parseListingElement(element)
		if err != nil {
			log.Printf("Failed to parse listing %d: %v", i, err)
			continue
		}

		if listing != nil {
			listings = append(listings, listing)
		}
	}

	log.Printf("Successfully parsed %d listings", len(listings))
	return listings, nil
}

// parseListingElement extracts data from a single listing element
func (p *AvitoParser) parseListingElement(element *rod.Element) (*models.Listing, error) {
	// Extract title
	titleElement, err := element.Element("[itemprop='name']")
	var title string
	if err != nil {
		// Try alternative selector
		titleElement, err = element.Element("[data-marker='item-title'] a")
		if err != nil {
			return nil, fmt.Errorf("title not found")
		}
	}
	title, err = titleElement.Text()
	if err != nil {
		return nil, fmt.Errorf("failed to get title text: %w", err)
	}
	title = strings.TrimSpace(title)

	// Extract price
	priceElement, err := element.Element("[itemprop='price'], [data-marker='item-price']")
	var price string
	if err != nil {
		price = "Price not specified"
	} else {
		price, err = priceElement.Text()
		if err != nil {
			price = "Price not available"
		} else {
			price = strings.TrimSpace(price)
		}
	}

	// Extract URL
	linkElement, err := element.Element("a[href]")
	var itemURL string
	if err == nil {
		href, err := linkElement.Attribute("href")
		if err == nil && href != nil {
			itemURL = *href
			if !strings.HasPrefix(itemURL, "http") {
				itemURL = "https://www.avito.ru" + itemURL
			}
		}
	}

	// Generate unique ID based on URL or title
	id := fmt.Sprintf("listing_%d", time.Now().UnixNano())
	if itemURL != "" {
		id = fmt.Sprintf("listing_%s", strings.ReplaceAll(itemURL, "/", "_"))
	}

	listing := &models.Listing{
		ID:        id,
		Title:     title,
		Price:     price,
		URL:       itemURL,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	return listing, nil
}

// SaveListing saves a listing to Redis
func (p *AvitoParser) SaveListing(listing *models.Listing) error {
	// Check if listing already exists
	exists, err := p.db.Exists(listing.ID)
	if err != nil {
		return fmt.Errorf("failed to check if listing exists: %w", err)
	}

	if exists {
		log.Printf("Listing %s already exists, skipping", listing.ID)
		return nil
	}

	// Convert to JSON
	data, err := listing.ToJSON()
	if err != nil {
		return fmt.Errorf("failed to convert listing to JSON: %w", err)
	}

	// Save to Redis with 24 hour expiration
	err = p.db.Set(listing.ID, string(data), 24*time.Hour)
	if err != nil {
		return fmt.Errorf("failed to save listing to Redis: %w", err)
	}

	log.Printf("Saved listing: %s - %s", listing.Title, listing.Price)
	return nil
}

// Close closes the browser
func (p *AvitoParser) Close() error {
	if p.browser != nil {
		return p.browser.Close()
	}
	return nil
}