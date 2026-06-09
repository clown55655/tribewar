package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"

	"tribeway/internal/database"
)

type Config struct {
	Database struct {
		MongoDB database.MongoConfig `yaml:"mongodb"`
	} `yaml:"database"`
}

func main() {
	configFile := flag.String("config", "config/config.yaml", "config file")
	direction := flag.String("direction", "up", "up or down")
	target := flag.Int("target", 0, "target version for down")
	flag.Parse()

	config, err := loadConfig(*configFile)
	if err != nil {
		fmt.Printf("load config: %v\n", err)
		os.Exit(1)
	}

	mongoManager, err := database.NewMongoManager(&config.Database.MongoDB)
	if err != nil {
		fmt.Printf("connect mongodb: %v\n", err)
		os.Exit(1)
	}
	defer mongoManager.Close()

	runner := database.NewMigrationRunner(mongoManager, defaultMigrations())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch *direction {
	case "up":
		err = runner.Up(ctx)
	case "down":
		err = runner.Down(ctx, *target)
	default:
		err = fmt.Errorf("unsupported direction: %s", *direction)
	}
	if err != nil {
		fmt.Printf("migration failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("migration completed")
}

func loadConfig(configFile string) (*Config, error) {
	viper.SetConfigFile(configFile)
	viper.SetConfigType("yaml")
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}
	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

func defaultMigrations() []database.Migration {
	return []database.Migration{
		{
			Version:     1,
			Name:        "audit_and_idempotency_indexes",
			Description: "create indexes for audit log and idempotent operation collections",
			Up: func(ctx context.Context, mm *database.MongoManager) error {
				return database.NewAuditRepository(mm).EnsureIndexes(ctx)
			},
			Down: func(ctx context.Context, mm *database.MongoManager) error {
				_, err := mm.GetCollection(database.AuditCollection).Indexes().DropAll(ctx)
				if err != nil {
					return err
				}
				_, err = mm.GetCollection(database.IdempotentCollection).Indexes().DropAll(ctx)
				return err
			},
		},
	}
}
