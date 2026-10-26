package store

// GameMetadata is the shared type returned by all store backends (Steam, GOG, manual).
type GameMetadata struct {
	Title            string
	ShortDescription string
	HeaderImageURL   string // remote URL to download
	PriceCents       *int   // nil if free
	Currency         string
	DiscountPercent  int
	IsFree           bool
	ReleaseDate      string
	HasWindows       bool
	HasLinux         bool
	HasMac           bool
	DRMFree          bool
	Genres           []string // e.g. "Action", "RPG"
	Tags             []string // e.g. "Co-op", "Multiplayer"
	StoreURL         string
	Source           string // "steam" or "gog"
	StoreProductID   string // Steam app ID or GOG product ID
}
