/*
 * Redis integration for AsteriskVoiceBridge
 *
 * Copyright (C) 2025, Sangoma Technologies Corporation
 *
 * This program is free software, distributed under the terms of
 * the GNU AFFERO General Public License Version 3. See the LICENSE file
 * at the top of the source tree.
 */

package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/exp/slog"
)

var log = slog.New(slog.NewTextHandler(os.Stderr, nil))

// AgentResponse represents a message from agent to client
type AgentResponse struct {
	Type           string `json:"type"`            // "text" or "sound"
	SourceLanguage string `json:"source_language"`
	TargetLanguage string `json:"target_language"`
	Text           string `json:"text"`
	File           string `json:"file"`
}

// ClientMessage represents a message from client to agent
type ClientMessage struct {
	AgentNo      string `json:"agentNo"`
	CallID       string `json:"callid"`
	OriginalText string `json:"original_text"`
	Translation  string `json:"translation"`
	Action       string `json:"action"`
	CallerNumber string `json:"caller_number"`
	Timestamp    int64  `json:"timestamp"`
}

// RedisClient handles Redis operations
type RedisClient struct {
	client *redis.Client
	ctx    context.Context
}

// Errors
var (
	ErrTimeout = errors.New("timeout")
	ErrNotFound = errors.New("not found")
)

// NewRedisClient creates a new Redis client
func NewRedisClient() (*RedisClient, bool) {
	// Get Redis configuration from environment variables
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	password := os.Getenv("REDIS_PASSWORD")
	
	dbStr := os.Getenv("REDIS_DB")
	db := 0
	if dbStr != "" {
		if parsedDB, err := strconv.Atoi(dbStr); err == nil {
			db = parsedDB
		}
	}

	// Create Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx := context.Background()

	// Test connection
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Error("Failed to connect to Redis", "error", err, "addr", addr)
		return nil, false
	}

	log.Info("Redis client connected", "addr", addr, "db", db)

	return &RedisClient{
		client: rdb,
		ctx:    ctx,
	}, true
}

// Close closes the Redis client
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// PopAgentResponse pops a message from agent response queue (BLPOP)
func (r *RedisClient) PopAgentResponse(queueKey string, timeout time.Duration) (*AgentResponse, error) {
	result, err := r.client.BLPop(r.ctx, timeout, queueKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrTimeout
		}
		return nil, err
	}

	if len(result) < 2 {
		return nil, fmt.Errorf("invalid BLPOP result: %v", result)
	}

	var response AgentResponse
	if err := json.Unmarshal([]byte(result[1]), &response); err != nil {
		log.Error("Failed to unmarshal agent response", "error", err, "data", result[1])
		return nil, err
	}

	log.Debug("Popped agent response", "queue", queueKey, "type", response.Type)
	return &response, nil
}

// PushClientMessage pushes a message to client message queue (RPUSH)
func (r *RedisClient) PushClientMessage(queueKey string, message ClientMessage) error {
	data, err := json.Marshal(message)
	if err != nil {
		log.Error("Failed to marshal client message", "error", err)
		return err
	}

	err = r.client.RPush(r.ctx, queueKey, data).Err()
	if err != nil {
		log.Error("Failed to push client message", "error", err, "queue", queueKey)
		return err
	}

	log.Debug("Pushed client message", "queue", queueKey, "original", message.OriginalText)
	return nil
}

// GetQueueLength returns the length of a Redis list
func (r *RedisClient) GetQueueLength(queueKey string) (int64, error) {
	return r.client.LLen(r.ctx, queueKey).Result()
}

// ClearQueue clears all messages from a Redis list
func (r *RedisClient) ClearQueue(queueKey string) error {
	return r.client.Del(r.ctx, queueKey).Err()
}

// HealthCheck checks if Redis connection is healthy
func (r *RedisClient) HealthCheck() error {
	_, err := r.client.Ping(r.ctx).Result()
	return err
}
