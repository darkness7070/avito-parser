package parser

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"avito-parser/internal/database"
	"avito-parser/internal/models"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type AvitoParser struct {
	browser   *rod.Browser
	db        *database.RedisClient
	headless  bool
	timeout   time.Duration
	baseURL   string
	cycleDelay time.Duration
	pageDelay time.Duration
}

// NewAvitoParser creates a new Avito parser instance
func NewAvitoParser(db *database.RedisClient, headless bool, timeout time.Duration, baseURL string, cycleDelay, pageDelay time.Duration) *AvitoParser {
	return &AvitoParser{
		db:        db,
		headless:  headless,
		timeout:   timeout,
		baseURL:   baseURL,
		cycleDelay: cycleDelay,
		pageDelay: pageDelay,
	}
}

// Start initializes the browser
func (p *AvitoParser) Start() error {
	var l *launcher.Launcher
	
	// Check if we have a custom browser path
	browserPath := os.Getenv("ROD_LAUNCHER_BIN")
	if browserPath != "" {
		l = launcher.New().Bin(browserPath)
	} else {
		l = launcher.New()
	}
	
	if p.headless {
		l = l.Headless(true).NoSandbox(true)
	} else {
		l = l.Headless(false)
	}
	
	// Add additional Chrome flags for better compatibility in containers
	l = l.Set("disable-gpu").
		Set("disable-dev-shm-usage").
		Set("disable-extensions").
		Set("no-first-run").
		Set("disable-background-timer-throttling").
		Set("disable-backgrounding-occluded-windows").
		Set("disable-renderer-backgrounding")

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

// generatePageURL generates URL for a specific page number
func (p *AvitoParser) generatePageURL(pageNum int) string {
	if pageNum == 1 {
		return p.baseURL
	}
	
	// Parse the base URL
	parsedURL, err := url.Parse(p.baseURL)
	if err != nil {
		log.Printf("Error parsing base URL: %v", err)
		return p.baseURL
	}
	
	// Add page parameter
	query := parsedURL.Query()
	query.Set("p", fmt.Sprintf("%d", pageNum))
	query.Set("localPriority", "0")
	parsedURL.RawQuery = query.Encode()
	
	return parsedURL.String()
}

// hasListings checks if page has listings (minimum threshold) with nil safety
func (p *AvitoParser) hasListings(pageURL string) (bool, int, error) {
	page, err := p.browser.Page(proto.TargetCreateTarget{URL: pageURL})
	if err != nil {
		return false, 0, fmt.Errorf("failed to create page: %w", err)
	}
	defer func() {
		if page != nil {
			page.Close()
		}
	}()

	// Wait for page to load
	err = page.WaitLoad()
	if err != nil {
		return false, 0, fmt.Errorf("failed to wait for page load: %w", err)
	}

	// Wait a bit for dynamic content
	time.Sleep(2 * time.Second)

	// Try to find listings with multiple selectors
	selectors := []string{
		"[data-marker='item']",
		"[data-marker*='item']",
		".item, .listing-item",
	}

	var listingElements rod.Elements
	for _, selector := range selectors {
		listingElements, err = page.Elements(selector)
		if err == nil && len(listingElements) > 0 {
			break
		}
	}
	
	if err != nil {
		log.Printf("Error finding listings on page: %v", err)
		return false, 0, nil
	}
	
	// Count valid (non-nil) elements
	validCount := 0
	for _, element := range listingElements {
		if element != nil {
			validCount++
		}
	}
	
	log.Printf("Found %d valid listings on page", validCount)
	return validCount >= 3, validCount, nil // Consider page valid if it has at least 3 listings
}

// ParseAllPages parses all available pages starting from page 1 with improved error handling
func (p *AvitoParser) ParseAllPages() error {
	log.Println("Starting full parsing cycle...")
	
	totalNewListings := 0
	totalPages := 0
	currentPage := 1
	maxRetries := 3
	
	for {
		pageURL := p.generatePageURL(currentPage)
		log.Printf("Processing page %d...", currentPage)
		
		// Check if page has enough listings with retry
		var hasListings bool
		var listingCount int
		var err error
		
		for retry := 0; retry < maxRetries; retry++ {
			hasListings, listingCount, err = p.hasListings(pageURL)
			if err == nil {
				break
			}
			log.Printf("Retry %d for page %d: %v", retry+1, currentPage, err)
			time.Sleep(2 * time.Second)
		}
		
		if err != nil {
			log.Printf("Failed to check page %d after %d retries: %v, skipping...", currentPage, maxRetries, err)
			currentPage++
			if currentPage > 10 { // Safety limit
				break
			}
			continue
		}
		
		if !hasListings {
			log.Printf("Found %d listings on page %d (less than minimum 3), ending pagination", listingCount, currentPage)
			break
		}
		
		// Parse the page with retry
		var listings []*models.Listing
		for retry := 0; retry < maxRetries; retry++ {
			listings, err = p.ParseListings(pageURL)
			if err == nil {
				break
			}
			log.Printf("Retry %d parsing page %d: %v", retry+1, currentPage, err)
			time.Sleep(2 * time.Second)
		}
		
		if err != nil {
			log.Printf("Failed to parse page %d after %d retries: %v, skipping...", currentPage, maxRetries, err)
			currentPage++
			continue
		}
		
		// Save listings
		newListingsCount := 0
		for _, listing := range listings {
			if listing == nil {
				continue // Skip nil listings
			}
			
			err := p.SaveListing(listing)
			if err != nil {
				if !strings.Contains(err.Error(), "already exists") {
					log.Printf("Error saving listing: %v", err)
				}
			} else {
				newListingsCount++
			}
		}
		
		log.Printf("Found %d listings on page %d, saved %d new listings", len(listings), currentPage, newListingsCount)
		totalNewListings += newListingsCount
		totalPages++
		
		// Delay before next page
		if p.pageDelay > 0 {
			time.Sleep(p.pageDelay)
		}
		
		currentPage++
		
		// Safety limit to prevent infinite loops
		if currentPage > 50 {
			log.Println("Reached maximum page limit (50), ending pagination")
			break
		}
	}
	
	log.Printf("Total cycle results: %d pages processed, %d new listings saved", totalPages, totalNewListings)
	return nil
}

// StartContinuousParsing starts continuous parsing with cycles
func (p *AvitoParser) StartContinuousParsing() {
	for {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Recovered from panic in parsing cycle: %v", r)
				}
			}()
			
			err := p.ParseAllPages()
			if err != nil {
				log.Printf("Error during parsing cycle: %v", err)
			}
		}()
		
		log.Printf("Waiting %v before next cycle...", p.cycleDelay)
		time.Sleep(p.cycleDelay)
	}
}

// ParseListings parses apartment listings from the given URL with nil safety
func (p *AvitoParser) ParseListings(url string) ([]*models.Listing, error) {
	page, err := p.browser.Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return nil, fmt.Errorf("failed to create page: %w", err)
	}
	defer func() {
		if page != nil {
			page.Close()
		}
	}()

	// Wait for page to load
	err = page.WaitLoad()
	if err != nil {
		return nil, fmt.Errorf("failed to wait for page load: %w", err)
	}

	// Wait a bit more for dynamic content
	time.Sleep(3 * time.Second)

	// Try multiple selectors to find listings
	selectors := []string{
		"[data-marker='item']",
		"[data-marker*='item']",
		".item, .listing-item",
	}

	var listingElements rod.Elements
	for _, selector := range selectors {
		listingElements, err = page.Elements(selector)
		if err == nil && len(listingElements) > 0 {
			log.Printf("Found %d elements with selector: %s", len(listingElements), selector)
			break
		}
	}

	if err != nil || len(listingElements) == 0 {
		log.Printf("No listing elements found on page")
		return []*models.Listing{}, nil
	}

	var listings []*models.Listing

	for i, element := range listingElements {
		if element == nil {
			log.Printf("Skipping nil element at index %d", i)
			continue
		}
		
		listing, err := p.parseListingElement(element)
		if err != nil {
			log.Printf("Failed to parse listing %d: %v", i, err)
			continue
		}

		if listing != nil {
			listings = append(listings, listing)
		}
	}

	log.Printf("Successfully parsed %d valid listings from %d elements", len(listings), len(listingElements))
	return listings, nil
}

// parseListingElement extracts data from a single listing element with nil safety
func (p *AvitoParser) parseListingElement(element *rod.Element) (*models.Listing, error) {
	if element == nil {
		return nil, fmt.Errorf("element is nil")
	}

	// Extract title with multiple selectors and nil checks
	titleSelectors := []string{
		"[itemprop='name']",
		"[data-marker='item-title'] a",
		"h3 a",
		".item-title a",
		"[data-marker*='title'] a",
		"a[title]",
	}

	var title string
	for _, selector := range titleSelectors {
		titleElement, err := element.Element(selector)
		if err == nil && titleElement != nil {
			title, err = titleElement.Text()
			if err == nil && strings.TrimSpace(title) != "" {
				title = strings.TrimSpace(title)
				break
			}
		}
	}

	if title == "" {
		return nil, fmt.Errorf("title not found or empty")
	}

	// Extract price with multiple selectors and nil checks
	priceSelectors := []string{
		"[itemprop='price']",
		"[data-marker='item-price']",
		".price",
		"[data-marker*='price']",
		".item-price",
	}

	var price string = "Price not specified"
	for _, selector := range priceSelectors {
		priceElement, err := element.Element(selector)
		if err == nil && priceElement != nil {
			priceText, err := priceElement.Text()
			if err == nil && strings.TrimSpace(priceText) != "" {
				price = strings.TrimSpace(priceText)
				break
			}
		}
	}

	// Extract URL with nil checks
	var itemURL string
	linkElement, err := element.Element("a[href]")
	if err == nil && linkElement != nil {
		href, err := linkElement.Attribute("href")
		if err == nil && href != nil && *href != "" {
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
	} else {
		// Use title hash as fallback
		id = fmt.Sprintf("listing_title_%d", len(title))
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

// SaveListing saves a listing to Redis with improved error handling
func (p *AvitoParser) SaveListing(listing *models.Listing) error {
	if listing == nil {
		return fmt.Errorf("listing is nil")
	}

	if listing.ID == "" {
		return fmt.Errorf("listing ID is empty")
	}

	// Check if listing already exists
	exists, err := p.db.Exists(listing.ID)
	if err != nil {
		return fmt.Errorf("failed to check if listing exists: %w", err)
	}

	if exists {
		// Don't log for existing listings to reduce noise
		return fmt.Errorf("listing already exists")
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