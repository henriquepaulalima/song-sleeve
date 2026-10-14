// Package storage is the ObjectStore interface over the S3-compatible bucket (§10.1).
// Clients only ever receive short-lived presigned grants: staging PUTs for uploads and
// final-key GETs for downloads (R-CLOUD-5, R-CLOUD-6).
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/henriquepaulalima/song-sleeve/server/internal/config"
)

// ErrTooLarge is returned when a download exceeds its byte limit (R-LIM-2).
var ErrTooLarge = errors.New("object exceeds the allowed size")

type Part struct {
	Number int32  `json:"part_number"`
	ETag   string `json:"etag"`
	Size   int64  `json:"-"`
}

type ObjectStore interface {
	CreateMultipart(ctx context.Context, key string) (uploadID string, err error)
	PresignUploadPart(ctx context.Context, key, uploadID string, part int32, ttl time.Duration) (string, error)
	ListParts(ctx context.Context, key, uploadID string) ([]Part, error)
	CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error
	AbortMultipart(ctx context.Context, key, uploadID string) error
	// Download streams key into w, failing with ErrTooLarge past maxBytes whatever
	// Content-Length says.
	Download(ctx context.Context, key string, w io.Writer, maxBytes int64) (int64, error)
	Put(ctx context.Context, key string, body io.ReadSeeker, size int64, contentType string) error
	Head(ctx context.Context, key string) (int64, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) (int, error)
	AbortUploadsUnder(ctx context.Context, prefix string) (int, error)
}

type S3 struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

func NewS3(ctx context.Context, cfg *config.Config) (*S3, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.S3Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKeyID, cfg.S3SecretAccessKey, "")),
		awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		awsconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		return nil, err
	}
	mk := func(endpoint string) *s3.Client {
		return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = cfg.S3PathStyle
		})
	}
	st := &S3{client: mk(cfg.S3Endpoint), presign: s3.NewPresignClient(mk(cfg.S3PublicEndpoint)), bucket: cfg.S3Bucket}
	if cfg.S3EnsureBucket {
		if err := st.ensureBucket(ctx, cfg.AllowedOrigins); err != nil {
			return nil, fmt.Errorf("ensure bucket: %w", err)
		}
	}
	return st, nil
}

// ensureBucket creates the development bucket with the CORS rule from §10.1:
// exact origins, PUT/GET/HEAD only, ETag exposed for multipart uploads.
func (s *S3) ensureBucket(ctx context.Context, origins []string) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: &s.bucket})
	if err != nil {
		if _, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &s.bucket}); err != nil {
			var owned *types.BucketAlreadyOwnedByYou
			if !errors.As(err, &owned) {
				return err
			}
		}
	}
	_, err = s.client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket: &s.bucket,
		CORSConfiguration: &types.CORSConfiguration{CORSRules: []types.CORSRule{{
			AllowedOrigins: origins,
			AllowedMethods: []string{"PUT", "GET", "HEAD"},
			AllowedHeaders: []string{"content-type"},
			ExposeHeaders:  []string{"ETag", "Content-Length"},
			MaxAgeSeconds:  aws.Int32(600),
		}}},
	})
	return err
}

func (s *S3) CreateMultipart(ctx context.Context, key string) (string, error) {
	out, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.UploadId), nil
}

func (s *S3) PresignUploadPart(ctx context.Context, key, uploadID string, part int32, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket: &s.bucket, Key: &key, UploadId: &uploadID, PartNumber: aws.Int32(part),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (s *S3) ListParts(ctx context.Context, key, uploadID string) ([]Part, error) {
	var parts []Part
	p := s3.NewListPartsPaginator(s.client, &s3.ListPartsInput{Bucket: &s.bucket, Key: &key, UploadId: &uploadID})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, pt := range page.Parts {
			parts = append(parts, Part{Number: aws.ToInt32(pt.PartNumber), ETag: aws.ToString(pt.ETag), Size: aws.ToInt64(pt.Size)})
		}
	}
	return parts, nil
}

func (s *S3) CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error {
	completed := make([]types.CompletedPart, len(parts))
	for i, p := range parts {
		completed[i] = types.CompletedPart{PartNumber: aws.Int32(p.Number), ETag: aws.String(p.ETag)}
	}
	_, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: &s.bucket, Key: &key, UploadId: &uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	})
	return err
}

func (s *S3) AbortMultipart(ctx context.Context, key, uploadID string) error {
	_, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: &s.bucket, Key: &key, UploadId: &uploadID})
	if isNotFound(err) {
		return nil
	}
	return err
}

func (s *S3) Download(ctx context.Context, key string, w io.Writer, maxBytes int64) (int64, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return 0, err
	}
	defer out.Body.Close()
	n, err := io.Copy(w, io.LimitReader(out.Body, maxBytes+1))
	if err != nil {
		return n, err
	}
	if n > maxBytes {
		return n, ErrTooLarge
	}
	return n, nil
}

func (s *S3) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, Body: body, ContentLength: aws.Int64(size), ContentType: aws.String(contentType),
	})
	return err
}

func (s *S3) Head(ctx context.Context, key string) (int64, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return 0, err
	}
	return aws.ToInt64(out.ContentLength), nil
}

func (s *S3) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
	if isNotFound(err) {
		return nil
	}
	return err
}

func (s *S3) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	n := 0
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: &s.bucket, Prefix: &prefix})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return n, err
		}
		for _, obj := range page.Contents {
			if err := s.Delete(ctx, aws.ToString(obj.Key)); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

func (s *S3) AbortUploadsUnder(ctx context.Context, prefix string) (int, error) {
	n := 0
	in := &s3.ListMultipartUploadsInput{Bucket: &s.bucket, Prefix: &prefix}
	for {
		out, err := s.client.ListMultipartUploads(ctx, in)
		if err != nil {
			return n, err
		}
		for _, u := range out.Uploads {
			if err := s.AbortMultipart(ctx, aws.ToString(u.Key), aws.ToString(u.UploadId)); err != nil {
				return n, err
			}
			n++
		}
		if !aws.ToBool(out.IsTruncated) {
			return n, nil
		}
		in.KeyMarker, in.UploadIdMarker = out.NextKeyMarker, out.NextUploadIdMarker
	}
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var nf *types.NotFound
	var nsk *types.NoSuchKey
	var nsu *types.NoSuchUpload
	if errors.As(err, &nf) || errors.As(err, &nsk) || errors.As(err, &nsu) {
		return true
	}
	var re interface{ HTTPStatusCode() int }
	if errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound {
		return true
	}
	return strings.Contains(err.Error(), "NoSuchUpload")
}

// Keys (§10.1). Final keys never receive a client grant.
func StagingKey(userID, operationID, attemptID string) string {
	return "users/" + userID + "/staging/" + operationID + "/" + attemptID
}

func FinalKey(userID, contentID string) string {
	return "users/" + userID + "/songs/" + contentID
}

func UserPrefix(userID string) string { return "users/" + userID + "/" }
