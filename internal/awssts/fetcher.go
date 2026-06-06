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

type Fetcher struct {
	store           keystore.Store
	roleARN         string
	roleSessionName string
	durationSeconds int
}

func NewFetcher(store keystore.Store, roleARNKey, roleSessionName string, durationSeconds int) (*Fetcher, error) {
	roleARN, err := store.Get(roleARNKey)
	if err != nil {
		return nil, fmt.Errorf("getting role ARN: %w", err)
	}
	if roleARN == "" {
		return nil, fmt.Errorf("role ARN is empty")
	}

	return &Fetcher{
		store:           store,
		roleARN:         roleARN,
		roleSessionName: roleSessionName,
		durationSeconds: durationSeconds,
	}, nil
}

func (f *Fetcher) FetchToken(ctx context.Context) (token string, expiry time.Time, err error) {
	// Load AWS config using standard credential chain
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("loading AWS config: %w", err)
	}

	client := sts.NewFromConfig(cfg)

	// Prepare duration (default 3600 seconds = 1 hour)
	duration := int32(f.durationSeconds)
	if duration <= 0 {
		duration = 3600
	}

	// Call AssumeRole
	result, err := client.AssumeRole(ctx, &sts.AssumeRoleInput{
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

	// AccessKeyId will be the primary token value
	accessKeyId := aws.ToString(creds.AccessKeyId)
	secretAccessKey := aws.ToString(creds.SecretAccessKey)
	sessionToken := aws.ToString(creds.SessionToken)

	// Store all three in keystore with prefix pattern
	// The calling code will handle the prefix from target_env_key
	if err := f.store.Set("_ACCESS_KEY_ID", accessKeyId); err != nil {
		return "", time.Time{}, fmt.Errorf("storing access key: %w", err)
	}
	if err := f.store.Set("_SECRET_ACCESS_KEY", secretAccessKey); err != nil {
		return "", time.Time{}, fmt.Errorf("storing secret key: %w", err)
	}
	if err := f.store.Set("_SESSION_TOKEN", sessionToken); err != nil {
		return "", time.Time{}, fmt.Errorf("storing session token: %w", err)
	}

	expiry = aws.ToTime(creds.Expiration)
	return accessKeyId, expiry, nil
}
