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

// hasListings checks if page has listings (minimum threshold)
func (p *AvitoParser) hasListings(pageURL string) (bool, int, error) {
	page, err := p.browser.Page(proto.TargetCreateTarget{URL: pageURL})
	if err != nil {
		return false, 0, fmt.Errorf("failed to create page: %w", err)
	}
	defer page.Close()

	// Wait for page to load
	err = page.WaitLoad()
	if err != nil {
		return false, 0, fmt.Errorf("failed to wait for page load: %w", err)
	}

	// Wait a bit for dynamic content
	time.Sleep(2 * time.Second)

	// Try to find listings
	listingElements, err := page.Elements("[data-marker='item']")
	if err != nil || len(listingElements) == 0 {
		// Try alternative selector
		listingElements, err = page.Elements("[data-marker*='item']")
		if err != nil {
			return false, 0, nil
		}
	}
	
	count := len(listingElements)
	return count >= 3, count, nil // Consider page valid if it has at least 3 listings
}

// ParseAllPages parses all available pages starting from page 1
func (p *AvitoParser) ParseAllPages() error {
	log.Println("Starting full parsing cycle...")
	
	totalNewListings := 0
	totalPages := 0
	currentPage := 1
	
	for {
		pageURL := p.generatePageURL(currentPage)
		log.Printf("Processing page %d...", currentPage)
		
		// Check if page has enough listings
		hasListings, listingCount, err := p.hasListings(pageURL)
		if err != nil {
			log.Printf("Error checking page %d: %v, skipping...", currentPage, err)
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
		
		// Parse the page
		listings, err := p.ParseListings(pageURL)
		if err != nil {
			log.Printf("Error parsing page %d: %v, skipping...", currentPage, err)
			currentPage++
			continue
		}
		
		// Save listings
		newListingsCount := 0
		for _, listing := range listings {
			err := p.SaveListing(listing)
			if err != nil {
				log.Printf("Error saving listing: %v", err)
			} else {
				// Check if it was actually saved (not a duplicate)
				if !strings.Contains(err.Error(), "already exists") {
					newListingsCount++
				}
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
		err := p.ParseAllPages()
		if err != nil {
			log.Printf("Error during parsing cycle: %v", err)
		}
		
		log.Printf("Waiting %v before next cycle...", p.cycleDelay)
		time.Sleep(p.cycleDelay)
	}
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

	// Wait a bit more for dynamic content
	time.Sleep(3 * time.Second)

	// Wait for listings to appear
	err = page.Timeout(p.timeout).WaitElementsMoreThan("[data-marker='item']", 0)
	if err != nil {
		log.Printf("Warning: No listings found with primary selector, trying alternative: %v", err)
		// Try alternative selector
		err = page.Timeout(p.timeout).WaitElementsMoreThan("[data-marker*='item']", 0)
		if err != nil {
			log.Printf("Warning: No listings found with any selector: %v", err)
			return []*models.Listing{}, nil
		}
	}

	// Find all listing elements
	listingElements, err := page.Elements("[data-marker='item']")
	if err != nil || len(listingElements) == 0 {
		// Try alternative selector
		listingElements, err = page.Elements("[data-marker*='item']")
		if err != nil {
			return nil, fmt.Errorf("failed to find listing elements: %w", err)
		}
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

	return listings, nil
}

// parseListingElement extracts data from a single listing element
func (p *AvitoParser) parseListingElement(element *rod.Element) (*models.Listing, error) {
	// Extract title
	titleElement, err := element.Element("[itemprop='name']")
	var title string
	if err != nil {
		// Try alternative selectors
		titleElement, err = element.Element("[data-marker='item-title'] a")
		if err != nil {
			titleElement, err = element.Element("h3 a, .item-title a, [data-marker*='title'] a")
			if err != nil {
				return nil, fmt.Errorf("title not found")
			}
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
		// Try alternative price selectors
		priceElement, err = element.Element(".price, [data-marker*='price'], .item-price")
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