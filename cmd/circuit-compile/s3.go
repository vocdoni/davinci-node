package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/vocdoni/vocdoni-z-sandbox/log"
)

// S3Config holds the configuration for S3 uploads
type S3Config struct {
	Enabled              bool
	HostBase             string
	HostBucket           string
	ServerSideEncryption bool
	AccessKey            string
	SecretKey            string
	Space                string
	Bucket               string
}

// NewDefaultS3Config returns a new S3Config with default values
func NewDefaultS3Config() *S3Config {
	return &S3Config{
		Enabled:              false,
		HostBase:             "ams3.digitaloceanspaces.com",
		HostBucket:           "%s.ams3.digitaloceanspaces.com",
		ServerSideEncryption: false,
		Space:                "circuits",
		Bucket:               "dev",
	}
}

// S3Uploader handles artifact uploads to S3
type S3Uploader struct {
	client *s3.Client
	config *S3Config
}

// NewS3Uploader creates a new S3Uploader with the provided configuration
func NewS3Uploader(cfg *S3Config) (*S3Uploader, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("s3 upload not enabled")
	}

	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("s3 access key and secret key are required")
	}

	// Create the AWS SDK configuration with credentials
	sdkConfig, err := config.LoadDefaultConfig(context.Background(),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			"", // Session token not used with DO Spaces
		)),
		config.WithRegion("us-east-1"), // This doesn't matter for DO Spaces but is required
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS SDK config: %w", err)
	}

	// Create the S3 client with the custom endpoint configuration for DigitalOcean Spaces
	client := s3.NewFromConfig(sdkConfig, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s", cfg.HostBase))
		o.UsePathStyle = true
	})

	return &S3Uploader{
		client: client,
		config: cfg,
	}, nil
}

// UploadFile uploads a file to the configured S3 bucket and returns the object key
func (u *S3Uploader) UploadFile(ctx context.Context, filePath string) (string, error) {
	// Read the file
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Warnw("failed to close file", "error", err)
		}
	}()

	// Get the filename for use as the object key
	fileName := filepath.Base(filePath)
	objectKey := fmt.Sprintf("%s/%s", u.config.Bucket, fileName)

	// Create the upload input
	uploadInput := &s3.PutObjectInput{
		Bucket: aws.String(u.config.Space),
		Key:    aws.String(objectKey),
		Body:   file,
	}

	// Upload the file
	log.Infow("uploading file to S3", "file", fileName, "space", u.config.Space, "bucket", u.config.Bucket)
	_, err = u.client.PutObject(ctx, uploadInput)
	if err != nil {
		return "", fmt.Errorf("failed to upload file %s: %w", filePath, err)
	}

	return objectKey, nil
}

// UploadDirectory uploads all files in a directory to the configured S3 bucket
// and returns a list of S3 object keys that were uploaded
func (u *S3Uploader) UploadDirectory(ctx context.Context, dirPath string) ([]string, error) {
	// Read the directory
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", dirPath, err)
	}

	// Track uploaded object keys
	uploadedKeys := []string{}

	// Upload each file
	for _, file := range files {
		if file.IsDir() {
			continue // Skip directories
		}
		filePath := filepath.Join(dirPath, file.Name())
		objectKey, err := u.UploadFile(ctx, filePath)
		if err != nil {
			return uploadedKeys, err
		}
		uploadedKeys = append(uploadedKeys, objectKey)
	}

	return uploadedKeys, nil
}

// SetPublicACL sets the ACL of specific objects to public-read
func (u *S3Uploader) SetPublicACL(ctx context.Context, objectKeys []string) error {
	if len(objectKeys) == 0 {
		log.Infow("no objects to set public ACL")
		return nil
	}

	// Set ACL for each specified object
	for _, key := range objectKeys {
		aclInput := &s3.PutObjectAclInput{
			Bucket: aws.String(u.config.Space),
			Key:    aws.String(key),
			ACL:    types.ObjectCannedACLPublicRead,
		}

		log.Infow("setting public ACL", "object", key)
		_, err := u.client.PutObjectAcl(ctx, aclInput)
		if err != nil {
			return fmt.Errorf("failed to set ACL for object %s: %w", key, err)
		}
	}

	return nil
}

// TestConnection tests the connection to the S3 service
func TestS3Connection(ctx context.Context, s3Config *S3Config) error {
	if !s3Config.Enabled {
		log.Infow("s3 upload not enabled, skipping connection test")
		return nil
	}

	log.Infow("testing S3 connection...")

	// Create uploader
	uploader, err := NewS3Uploader(s3Config)
	if err != nil {
		return fmt.Errorf("failed to create S3 uploader: %w", err)
	}

	// Try to list objects to test the connection
	listInput := &s3.ListObjectsV2Input{
		Bucket:  aws.String(s3Config.Space),
		MaxKeys: aws.Int32(1), // Only need 1 object to verify connection
	}

	_, err = uploader.client.ListObjectsV2(ctx, listInput)
	if err != nil {
		return fmt.Errorf("S3 connection test failed: %w", err)
	}

	log.Infow("S3 connection successful",
		"host", s3Config.HostBase,
		"space", s3Config.Space,
		"bucket", s3Config.Bucket)
	return nil
}

// UploadFiles uploads a list of specific files to S3 and makes them public
func UploadFiles(ctx context.Context, filePaths []string, s3Config *S3Config) error {
	if !s3Config.Enabled {
		log.Infow("s3 upload not enabled, skipping")
		return nil
	}

	if len(filePaths) == 0 {
		log.Infow("no files to upload")
		return nil
	}

	// Create uploader
	uploader, err := NewS3Uploader(s3Config)
	if err != nil {
		return fmt.Errorf("failed to create S3 uploader: %w", err)
	}

	// Track uploaded object keys
	uploadedKeys := []string{}

	// Upload each specified file
	for _, filePath := range filePaths {
		log.Infow("uploading file to S3", "file", filePath)
		objectKey, err := uploader.UploadFile(ctx, filePath)
		if err != nil {
			return fmt.Errorf("failed to upload file %s: %w", filePath, err)
		}
		uploadedKeys = append(uploadedKeys, objectKey)
	}

	// Set ACL to public-read only for the files just uploaded
	log.Infow("setting public ACL for uploaded artifacts", "count", len(uploadedKeys))
	if err := uploader.SetPublicACL(ctx, uploadedKeys); err != nil {
		return fmt.Errorf("failed to set public ACL: %w", err)
	}

	log.Infow("artifacts successfully uploaded to S3", "count", len(uploadedKeys))
	return nil
}
