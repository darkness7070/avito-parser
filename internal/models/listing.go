package models

import (
	"encoding/json"
	"time"
)

// Listing represents an apartment listing from Avito
type Listing struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Price       string    `json:"price"`
	URL         string    `json:"url"`
	Location    string    `json:"location,omitempty"`
	Description string    `json:"description,omitempty"`
	Images      []string  `json:"images,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ToJSON converts the listing to JSON string
func (l *Listing) ToJSON() ([]byte, error) {
	return json.Marshal(l)
}

// FromJSON creates a listing from JSON data
func FromJSON(data []byte) (*Listing, error) {
	var listing Listing
	err := json.Unmarshal(data, &listing)
	return &listing, err
}