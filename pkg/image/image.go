package image

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Could not download image: Got status %d %s", resp.StatusCode, resp.Status)
	}
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
		return "", errors.Wrap(err, "Could not create serving URL")
	}
	servingURL, err := image.ServingURL(ctx, blobKey, &image.ServingURLOptions{
		Secure: true,
		Size:   450,
	})
	if err != nil {
		return "", errors.Wrap(err, "Could not create serving URL")
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

// SaveURLToDB stores the serving URL and the GCS image location in the database
func (img *Image) SaveURLToDB(ctx context.Context, db *sql.Conn) error {
	parts := strings.Split(img.FileName, "/")
	ID := parts[len(parts)-1]
	_, err := db.ExecContext(ctx,
		"UPDATE products SET image_url = ?, image_location = ? WHERE id = ?",
		img.ServingURL, img.FileName, ID)
	if err != nil {
		return errors.Wrapf(err, "Saving image URL and location failed")
	}
	return nil
}
