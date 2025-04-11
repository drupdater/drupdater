package internal

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

type Repository interface {
	Push(o *git.PushOptions) error
	Head() (*plumbing.Reference, error)
	CommitObject(h plumbing.Hash) (*object.Commit, error)
	References() (storer.ReferenceIter, error)
}

type Worktree interface {
	Add(path string) (plumbing.Hash, error)
	AddGlob(pattern string) error
	Remove(path string) (plumbing.Hash, error)
	Commit(msg string, opts *git.CommitOptions) (plumbing.Hash, error)
	Status() (git.Status, error)
	Checkout(opts *git.CheckoutOptions) error
}

type S3Client interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

type DynamoDBClient interface {
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
}
