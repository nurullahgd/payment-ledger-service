package domain

type MerchantStatus string

const (
	MerchantStatusActive    MerchantStatus = "active"
	MerchantStatusSuspended MerchantStatus = "suspended"
)

type Merchant struct {
	ID         string
	Name       string
	APIKey     string
	Currency   string
	Status     MerchantStatus
	WebhookURL string
}

func (m *Merchant) IsActive() bool {
	return m.Status == MerchantStatusActive
}
