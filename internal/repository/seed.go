package repository

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
)

func SeedData(ctx context.Context, db *pgxpool.Pool, tenantRepo *TenantRepository) error {
	publicSchemaSQL := `
	CREATE TABLE IF NOT EXISTS public.merchants (
		id VARCHAR(50) PRIMARY KEY,
		name VARCHAR(100) NOT NULL,
		api_key VARCHAR(100) UNIQUE NOT NULL,
		currency VARCHAR(3) NOT NULL,
		status VARCHAR(20) DEFAULT 'active',
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);`

	if _, err := db.Exec(ctx, publicSchemaSQL); err != nil {
		return fmt.Errorf("failed to create public.merchants table: %w", err)
	}

	merchants := []struct {
		ID       string
		Name     string
		APIKey   string
		Currency string
	}{
		{"merchant_1", "Test Merchant One", "sk_test_12345", "USD"},
		{"merchant_2", "Test Merchant Two", "sk_test_67890", "EUR"},
	}

	for _, m := range merchants {
		insertSQL := `
		INSERT INTO public.merchants (id, name, api_key, currency, status)
		VALUES ($1, $2, $3, $4, 'active')
		ON CONFLICT (id) DO NOTHING;`

		_, err := db.Exec(ctx, insertSQL, m.ID, m.Name, m.APIKey, m.Currency)
		if err != nil {
			return fmt.Errorf("failed to insert merchant %s: %w", m.ID, err)
		}

		if err := tenantRepo.CreateTenantSchema(ctx, m.ID); err != nil {
			return fmt.Errorf("failed to create tenant schema for %s: %w", m.ID, err)
		}
	}

	log.Println("Seed data executed: Test merchants and tenant schemas are ready.")
	return nil
}
