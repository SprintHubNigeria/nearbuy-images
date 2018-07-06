package image

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/pkg/errors"

	"google.golang.org/appengine/blobstore"
	"google.golang.org/appengine/image"

	"golang.org/x/net/context"

	"cloud.google.com/go/storage"
)

var allowedImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpg":  true,
	"image/jpeg": true,
}

// Image holds data about images stored in GCS
type Image struct {
	FileName    string
	OriginalURL string
	ServingURL  string
	Data        []byte
	ContentType string
}

type filename string

// FileName is a context key for the image file name
var FileName = filename("fileName")

// DownloadImage fetches the image from the source url
func DownloadImage(ctx context.Context, client *http.Client, url, fileName string) (*Image, error) {
	if fileName == "" {
		return nil, fmt.Errorf("No file name given")
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, errors.Wrap(err, "Could not download image")
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrapf(err, "Could not read image body: Status - %d %s", resp.StatusCode, resp.Status)
	}
	return &Image{
		FileName:    fileName,
		ContentType: resp.Header.Get("Content-Type"),
		Data:        b,
		OriginalURL: url,
	}, nil
}

// SaveToGCS saves the image downloaded to cloud storage
func (img *Image) SaveToGCS(ctx context.Context, bucketName string) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	wc := client.Bucket(bucketName).Object(img.FileName).NewWriter(ctx)
	wc.ContentType = "image/jpeg"
	wc.Metadata = map[string]string{
		"x-goog-meta-source": img.OriginalURL,
	}
	if _, err := wc.Write(img.Data); err != nil {
		return errors.Wrapf(err, "Could not write image to file %q", img.FileName)
	}
	if err := wc.Close(); err != nil {
		return errors.Wrapf(err, "Could not close bucket %q, file %q", bucketName, img.FileName)
	}
	return nil
}

// CreateServingURL returns a serving URL for an image in cloud storage
func (img *Image) CreateServingURL(ctx context.Context, bucketName string) (string, error) {
	blobKey, err := blobstore.BlobKeyForFile(ctx, fmt.Sprintf("/gs/%s/%s", bucketName, img.FileName))
	if err != nil {
		return "", err
	}
	servingURL, err := image.ServingURL(ctx, blobKey, &image.ServingURLOptions{
		Secure: true,
		Size:   450,
	})
	if err != nil {
		return "", err
	}
	img.ServingURL = servingURL.String()
	return img.ServingURL, nil
}

// DeleteServingURL makes the serving URL unavailable
func (img *Image) DeleteServingURL(ctx context.Context, bucketName string) error {
	key, err := blobstore.BlobKeyForFile(ctx, fmt.Sprintf("/gs/%s/%s", bucketName, img.FileName))
	if err != nil {
		return err
	}
	return image.DeleteServingURL(ctx, key)
}

// DeleteFromGCS removes the image from cloud storage
func (img *Image) DeleteFromGCS(ctx context.Context, bucketName string) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	return client.Bucket(bucketName).Object(img.FileName).Delete(ctx)
}
