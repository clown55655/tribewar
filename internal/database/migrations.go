package database

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const MigrationCollection = "schema_migrations"

type Migration struct {
	Version     int
	Name        string
	Up          func(context.Context, *MongoManager) error
	Down        func(context.Context, *MongoManager) error
	Description string
}

type MigrationRecord struct {
	Version   int       `bson:"version" json:"version"`
	Name      string    `bson:"name" json:"name"`
	AppliedAt time.Time `bson:"applied_at" json:"applied_at"`
}

type MigrationRunner struct {
	mongo      *MongoManager
	migrations []Migration
}

func NewMigrationRunner(mm *MongoManager, migrations []Migration) *MigrationRunner {
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})
	return &MigrationRunner{mongo: mm, migrations: migrations}
}

func (r *MigrationRunner) Up(ctx context.Context) error {
	if err := r.ensureIndex(ctx); err != nil {
		return err
	}
	applied, err := r.appliedVersions(ctx)
	if err != nil {
		return err
	}
	for _, migration := range r.migrations {
		if applied[migration.Version] {
			continue
		}
		if migration.Up == nil {
			return fmt.Errorf("migration %d has no up function", migration.Version)
		}
		if err := migration.Up(ctx, r.mongo); err != nil {
			return fmt.Errorf("apply migration %d %s: %w", migration.Version, migration.Name, err)
		}
		if _, err := r.collection().InsertOne(ctx, &MigrationRecord{
			Version:   migration.Version,
			Name:      migration.Name,
			AppliedAt: time.Now(),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *MigrationRunner) Down(ctx context.Context, targetVersion int) error {
	applied, err := r.appliedVersions(ctx)
	if err != nil {
		return err
	}
	for i := len(r.migrations) - 1; i >= 0; i-- {
		migration := r.migrations[i]
		if migration.Version <= targetVersion || !applied[migration.Version] {
			continue
		}
		if migration.Down == nil {
			return fmt.Errorf("migration %d has no down function", migration.Version)
		}
		if err := migration.Down(ctx, r.mongo); err != nil {
			return fmt.Errorf("rollback migration %d %s: %w", migration.Version, migration.Name, err)
		}
		if _, err := r.collection().DeleteOne(ctx, bson.M{"version": migration.Version}); err != nil {
			return err
		}
	}
	return nil
}

func (r *MigrationRunner) collection() *mongo.Collection {
	return r.mongo.GetCollection(MigrationCollection)
}

func (r *MigrationRunner) ensureIndex(ctx context.Context) error {
	_, err := r.collection().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "version", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}

func (r *MigrationRunner) appliedVersions(ctx context.Context) (map[int]bool, error) {
	cursor, err := r.collection().Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	applied := make(map[int]bool)
	for cursor.Next(ctx) {
		var record MigrationRecord
		if err := cursor.Decode(&record); err != nil {
			return nil, err
		}
		applied[record.Version] = true
	}
	return applied, cursor.Err()
}
