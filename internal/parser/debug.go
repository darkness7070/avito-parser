package parser

import (
	"log"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// DebugPage analyzes page structure for debugging
func (p *AvitoParser) DebugPage(url string) error {
	log.Printf("=== DEBUG MODE: Analyzing page structure ===")
	log.Printf("URL: %s", url)

	page, err := p.browser.Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return err
	}
	defer page.Close()

	// Wait for page to load
	err = page.WaitLoad()
	if err != nil {
		log.Printf("Failed to wait for page load: %v", err)
		return err
	}

	// Wait for content
	log.Println("Waiting for page content...")
	time.Sleep(5 * time.Second)

	// Get page title
	title, err := page.Title()
	if err == nil {
		log.Printf("Page title: %s", title)
	} else {
		log.Printf("Failed to get page title: %v", err)
	}

	// Check for common selectors
	selectors := []string{
		"[data-marker='item']",
		"[data-marker*='item']",
		".item",
		".listing-item",
		"[data-marker='catalog-serp']",
		"[data-marker*='catalog']",
		"article",
		".js-catalog_after-ads",
		"[data-marker*='snippet']",
	}

	for _, selector := range selectors {
		elements, err := page.Elements(selector)
		if err == nil {
			log.Printf("Selector '%s': found %d elements", selector, len(elements))
			if len(elements) > 0 {
				// Try to get text from first element
				if elements[0] != nil {
					text, err := elements[0].Text()
					if err == nil && len(text) > 0 {
						if len(text) > 100 {
							text = text[:100] + "..."
						}
						log.Printf("  First element text: %s", text)
					}
				}
			}
		} else {
			log.Printf("Selector '%s': error %v", selector, err)
		}
	}

	// Check if we're blocked or redirected
	body, err := page.Element("body")
	if err == nil && body != nil {
		bodyText, err := body.Text()
		if err == nil {
			if len(bodyText) < 500 {
				log.Printf("Body text (full): %s", bodyText)
			} else {
				log.Printf("Body text (first 500 chars): %s...", bodyText[:500])
			}
			
			// Check for common blocking indicators
			blockingKeywords := []string{
				"блокировка",
				"доступ запрещен",
				"access denied",
				"captcha",
				"проверка браузера",
				"robot",
				"бот",
			}
			
			for _, keyword := range blockingKeywords {
				if containsIgnoreCase(bodyText, keyword) {
					log.Printf("⚠️  WARNING: Page might be blocked - found keyword: %s", keyword)
				}
			}
		}
	}

	// Take screenshot for manual inspection
	screenshot, err := page.Screenshot(true, nil)
	if err == nil {
		log.Printf("Screenshot taken, size: %d bytes", len(screenshot))
	} else {
		log.Printf("Failed to take screenshot: %v", err)
	}

	log.Printf("=== END DEBUG ===")
	return nil
}

// containsIgnoreCase checks if string contains substring (case insensitive)
func containsIgnoreCase(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if toLower(s[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// toLower converts byte to lowercase
func toLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	if b >= 'А' && b <= 'Я' {
		return b + ('а' - 'А')
	}
	return b
}