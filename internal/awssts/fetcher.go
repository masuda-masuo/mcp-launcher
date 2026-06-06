package awssts

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/masuda-masuo/mcp-launcher/internal/keystore"
)

// STSClient defines the minimal interface for AWS STS operations
type STSClient interface {
	AssumeRole(ctx context.Context, input *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error)
}

type Fetcher struct {
	store           keystore.Store
	client          STSClient
	roleARN         string
	roleSessionName string
	durationSeconds int
	targetEnvKey    string // prefix for keystore keys (e.g., "AWS")
}

func NewFetcher(ctx context.Context, store keystore.Store, roleARNKey, roleSessionName, targetEnvKey string, durationSeconds int) (*Fetcher, error) {
	roleARN, err := store.Get(roleARNKey)
	if err != nil {
		return nil, fmt.Errorf("getting role ARN: %w", err)
	}
	if roleARN == "" {
		return nil, fmt.Errorf("role ARN is empty")
	}

	// Load AWS config once during initialization
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	client := sts.NewFromConfig(cfg)

	return &Fetcher{
		store:           store,
		client:          client,
		roleARN:         roleARN,
		roleSessionName: roleSessionName,
		durationSeconds: durationSeconds,
		targetEnvKey:    targetEnvKey,
	}, nil
}

// NewFetcherWithClient creates a Fetcher with an explicit STS client (useful for testing)
func NewFetcherWithClient(store keystore.Store, roleARNKey, roleSessionName, targetEnvKey string, durationSeconds int, client STSClient) (*Fetcher, error) {
	roleARN, err := store.Get(roleARNKey)
	if err != nil {
		return nil, fmt.Errorf("getting role ARN: %w", err)
	}
	if roleARN == "" {
		return nil, fmt.Errorf("role ARN is empty")
	}

	return &Fetcher{
		store:           store,
		client:          client,
		roleARN:         roleARN,
		roleSessionName: roleSessionName,
		durationSeconds: durationSeconds,
		targetEnvKey:    targetEnvKey,
	}, nil
}

func (f *Fetcher) FetchToken(ctx context.Context) (token string, expiry time.Time, err error) {
	// Prepare duration (default 3600 seconds = 1 hour)
	duration := int32(f.durationSeconds)
	if duration <= 0 {
		duration = 3600
	}

	// Call AssumeRole
	result, err := f.client.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn:         aws.String(f.roleARN),
		RoleSessionName: aws.String(f.roleSessionName),
		DurationSeconds: aws.Int32(duration),
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("assuming role: %w", err)
	}

	creds := result.Credentials
	if creds == nil {
		return "", time.Time{}, fmt.Errorf("no credentials in response")
	}

	// Extract credential values
	accessKeyId := aws.ToString(creds.AccessKeyId)
	secretAccessKey := aws.ToString(creds.SecretAccessKey)
	sessionToken := aws.ToString(creds.SessionToken)

	// Store all three credentials in keystore with target_env_key as prefix
	// e.g., if targetEnvKey="AWS": "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"
	accessKeyIdKey := f.targetEnvKey + "_ACCESS_KEY_ID"
	secretAccessKeyKey := f.targetEnvKey + "_SECRET_ACCESS_KEY"
	sessionTokenKey := f.targetEnvKey + "_SESSION_TOKEN"

	if err := f.store.Set(accessKeyIdKey, accessKeyId); err != nil {
		return "", time.Time{}, fmt.Errorf("storing access key: %w", err)
	}
	if err := f.store.Set(secretAccessKeyKey, secretAccessKey); err != nil {
		return "", time.Time{}, fmt.Errorf("storing secret key: %w", err)
	}
	if err := f.store.Set(sessionTokenKey, sessionToken); err != nil {
		return "", time.Time{}, fmt.Errorf("storing session token: %w", err)
	}

	expiry = aws.ToTime(creds.Expiration)
	return accessKeyId, expiry, nil
}
