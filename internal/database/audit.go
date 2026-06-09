package database

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	AuditCollection      = "audit_logs"
	IdempotentCollection = "idempotent_operations"
)

type AuditLog struct {
	ID        string                 `bson:"_id,omitempty" json:"id,omitempty"`
	RequestID string                 `bson:"request_id" json:"request_id"`
	UserID    uint64                 `bson:"user_id" json:"user_id"`
	Action    string                 `bson:"action" json:"action"`
	TargetID  uint64                 `bson:"target_id" json:"target_id"`
	Status    string                 `bson:"status" json:"status"`
	Details   map[string]interface{} `bson:"details,omitempty" json:"details,omitempty"`
	CreatedAt time.Time              `bson:"created_at" json:"created_at"`
}

type IdempotentOperation struct {
	ID        string    `bson:"_id" json:"id"`
	Action    string    `bson:"action" json:"action"`
	Status    string    `bson:"status" json:"status"`
	Result    []byte    `bson:"result,omitempty" json:"result,omitempty"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

type AuditRepository struct {
	auditCollection      *mongo.Collection
	idempotentCollection *mongo.Collection
}

func NewAuditRepository(mm *MongoManager) *AuditRepository {
	return &AuditRepository{
		auditCollection:      mm.GetCollection(AuditCollection),
		idempotentCollection: mm.GetCollection(IdempotentCollection),
	}
}

func (r *AuditRepository) Log(ctx context.Context, log *AuditLog) error {
	if log == nil {
		return fmt.Errorf("audit log is nil")
	}
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now()
	}
	_, err := r.auditCollection.InsertOne(ctx, log)
	return err
}

func (r *AuditRepository) BeginIdempotent(ctx context.Context, key, action string) (bool, error) {
	now := time.Now()
	op := &IdempotentOperation{
		ID:        key,
		Action:    action,
		Status:    "processing",
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := r.idempotentCollection.InsertOne(ctx, op)
	if err == nil {
		return true, nil
	}
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	return false, err
}

func (r *AuditRepository) CompleteIdempotent(ctx context.Context, key string, result []byte) error {
	_, err := r.idempotentCollection.UpdateOne(
		ctx,
		bson.M{"_id": key},
		bson.M{"$set": bson.M{
			"status":     "completed",
			"result":     result,
			"updated_at": time.Now(),
		}},
	)
	return err
}

func (r *AuditRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.auditCollection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "request_id", Value: 1}, {Key: "action", Value: 1}},
	})
	if err != nil {
		return err
	}
	_, err = r.idempotentCollection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "updated_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(7 * 24 * 3600),
	})
	return err
}
